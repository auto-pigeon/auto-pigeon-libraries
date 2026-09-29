/**
 * Shared plumbing for the schema tests. No test cases live here.
 *
 * `ajv/dist/2020` is deliberately the validator: it is what Auto-Pigeon's own APMap pipeline
 * compiles this schema with, so a construct that passes here passes in the consumer that matters.
 */
import Ajv2020 from 'ajv/dist/2020.js';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const PACKAGE_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

export const readJson = (file) => JSON.parse(fs.readFileSync(file, 'utf8'));

/**
 * THE CONTRACT BUNDLE — discovered, never named.
 *
 * `schema/` holds exactly one `apmap-*.schema.json`, and its FILENAME carries the CURRENT version.
 * `schema/deprecated/` holds zero or more `apmap-*.schema.json`, and each of those is a supported
 * LEGACY READ contract. Nothing in this repository or any consumer hard-codes "1.0" or "1.1":
 * every service derives both by the same directory reads below, so promoting 1.2 is one file
 * rename plus one file move, and no code change anywhere.
 *
 * The layout IS the policy:
 *
 *     schema/apmap-<v>.schema.json              CURRENT — the only WRITE format
 *     schema/deprecated/apmap-<v>.schema.json   LEGACY  — readable, never written
 *
 * Depth is what separates the two, and it is the mechanism rather than an optimisation: the
 * current-schema scan reads DIRECT children only, so a deprecated schema can never be mistaken for
 * the writer's contract. It is loadable, deliberately — "deprecated" means "not current / never
 * written", not "unreadable". A format that becomes genuinely unreadable moves OUT of this tree.
 */
export const SCHEMA_DIR = path.join(PACKAGE_ROOT, 'schema');
export const DEPRECATED_SCHEMA_DIR = path.join(SCHEMA_DIR, 'deprecated');
export const CURRENT_SCHEMA_PATTERN = /^apmap-(\d+\.\d+)\.schema\.json$/;

/** Every direct child of `schema/` that looks like a current schema. Exactly one is legal. */
export function currentSchemaFiles(directory = SCHEMA_DIR) {
  return fs.readdirSync(directory, { withFileTypes: true })
    .filter((entry) => entry.isFile() && CURRENT_SCHEMA_PATTERN.test(entry.name))
    .map((entry) => entry.name)
    .sort();
}

export function currentSchemaPath(directory = SCHEMA_DIR) {
  const found = currentSchemaFiles(directory);
  if (found.length !== 1)
    throw new Error(`expected exactly one current schema in ${directory}, found ${found.length}: ${found.join(', ')}`);
  return path.join(directory, found[0]);
}

/** The current version, derived from the filename rather than declared anywhere. */
export function currentVersion(directory = SCHEMA_DIR) {
  return CURRENT_SCHEMA_PATTERN.exec(path.basename(currentSchemaPath(directory)))[1];
}

export const loadCurrentSchema = (directory = SCHEMA_DIR) => readJson(currentSchemaPath(directory));

/** Every `apmap-*.schema.json` directly under `<directory>/deprecated/`. Zero or more are legal. */
export function legacySchemaFiles(directory = SCHEMA_DIR) {
  const deprecated = path.join(directory, 'deprecated');
  if (!fs.existsSync(deprecated)) return [];
  return fs.readdirSync(deprecated, { withFileTypes: true })
    .filter((entry) => entry.isFile() && CURRENT_SCHEMA_PATTERN.test(entry.name))
    .map((entry) => entry.name)
    .sort();
}

/**
 * THE REFERENCE BUNDLE LOADER — the shape every service's startup loader implements.
 *
 * It lives here, in the contract authority's tests, because four services reimplement it in three
 * languages and the one place they can be checked against each other is the package that defines
 * what they are reading. It is test-only: this package ships schemas and no runtime code.
 *
 *     require exactly one current schema -> version from its filename -> parse -> the schema must
 *     be able to express that version -> enumerate deprecated/ -> same four steps each -> refuse a
 *     duplicate version -> cache { current, readable }
 *
 * Every fault throws. A legacy contract this bundle ADVERTISES as readable but cannot parse is a
 * startup failure, not a feature-time surprise: a service that claims to read 1.0 and discovers at
 * the user's first import that its 1.0 contract is corrupt has lied about its capability.
 */
export function loadContractBundle(directory = SCHEMA_DIR) {
  const currentFile = currentSchemaFiles(directory);
  if (currentFile.length !== 1)
    throw new Error(`expected exactly one current APMap schema in ${directory}, found ${currentFile.length}`
      + (currentFile.length ? `: ${currentFile.join(', ')}` : '')
      + '; exactly one file is the contract, and a second is an ambiguity nothing can resolve');

  const readable = new Map();
  const current = readSchemaFile(path.join(directory, currentFile[0]));
  readable.set(current.version, current);

  for (const name of legacySchemaFiles(directory)) {
    const legacy = readSchemaFile(path.join(directory, 'deprecated', name));
    if (readable.has(legacy.version))
      throw new Error(`APMap ${legacy.version} is declared twice in ${directory}; a version that `
        + 'resolves to two contracts resolves to neither');
    readable.set(legacy.version, legacy);
  }
  return { current, readable };
}

/** One schema file: parsed, version-from-filename, and checked that it can say its own name. */
function readSchemaFile(file) {
  const name = path.basename(file);
  const version = CURRENT_SCHEMA_PATTERN.exec(name)[1];
  let schema;
  try {
    schema = readJson(file);
  } catch (cause) {
    throw new Error(`the APMap schema ${file} is not valid JSON: ${cause.message}`);
  }
  const contract = schema?.properties?.apmap_version ?? {};
  const accepted = contract.const ? [contract.const] : (contract.enum ?? []);
  if (!accepted.includes(version))
    throw new Error(`${name} is named for APMap ${version}, but its apmap_version contract `
      + `${JSON.stringify(accepted)} cannot express it`);
  return { version, path: file, schema };
}

/** The versions this bundle can READ, sorted. The current one is always among them. */
export const readableVersions = (directory = SCHEMA_DIR) =>
  [...loadContractBundle(directory).readable.keys()].sort();

/** One Ajv instance per call: a compiled validator caches, and these tests compile several schemas. */
export const compile = (schema) =>
  new Ajv2020({ allErrors: true, strict: false, allowUnionTypes: true }).compile(schema);

/**
 * A validator for one `$defs` entry of a schema, so a negative vector can be checked against the
 * definition it actually violates. Validating a whole document against a nested `oneOf` reports
 * every branch's failure at once, which says the document is wrong without saying why.
 */
export const compileDef = (schema, def) =>
  new Ajv2020({ allErrors: true, strict: false, allowUnionTypes: true })
    .addSchema(schema, 'apmap')
    .compile({ $ref: `apmap#/$defs/${def}` });

/** Resolve a JSON Pointer. Empty string is the document itself. */
export const resolvePointer = (document, pointer) =>
  pointer === '' ? document
    : pointer.split('/').slice(1).reduce((node, token) => node[token.replace(/~1/g, '/').replace(/~0/g, '~')], document);

/** The CURRENT conformance vectors. There is one set, for the one contract. */
export const vectorIndex = () => readJson(path.join(PACKAGE_ROOT, 'test-vectors', 'index.json'));
export const vector = (kind, file) => readJson(path.join(PACKAGE_ROOT, 'test-vectors', kind, file));

/**
 * The DEPRECATED corpus — `deprecated/1.0/`, the published 1.0 vectors and examples. Reachable by
 * an explicit repository-local path only; it is history, not a runtime input.
 *
 * `deprecatedSchemaPath()` is different in kind: it names the frozen 1.0 CONTRACT, which IS a
 * runtime input — a reader loads it to validate a 1.0 document it has been asked to open. It is
 * outside `exports` because no writer may ever choose it, not because nothing may read it.
 */
export const DEPRECATED_ROOT = path.join(PACKAGE_ROOT, 'deprecated');
export const deprecatedSchemaPath = () => path.join(SCHEMA_DIR, 'deprecated', 'apmap-1.0.schema.json');
export const deprecated10 = (...segments) => path.join(DEPRECATED_ROOT, '1.0', ...segments);

/**
 * THE DOCUMENTED PROMOTION, as the contract defines it — the one place these tests express it.
 *
 * A legacy document becomes a current one by rewriting the declared version and supplying the
 * structural defaults the current contract requires and the old one had no place for. Today that
 * is exactly one member: `groups`, which 1.2 requires of every writer and 1.0/1.1 could not say.
 *
 * What makes this evidence rather than a formality is what it does NOT do. It copies every other
 * member by reference — no geometry is rebuilt, no id is reminted, no provenance is rewritten — so
 * a promotion that passes here is provably a header-and-default change and nothing more. When a
 * future version needs a second default, it is added here and every migration test moves with it.
 *
 * 1.4 needed none. Its additions — `authorship` and the `group` member arm — are optional, so a
 * promoted document gains no author, no licence and no nesting it never declared; its one
 * relaxation (a group may hold one member) only admits documents 1.3 refused, so every valid 1.3
 * group is a valid 1.4 group byte for byte.
 */
export const promoteToCurrent = (document, version = currentVersion()) => ({
  ...document,
  apmap_version: version,
  groups: document.groups ?? [],
});

/**
 * THE REFERENCE SEMANTIC CHECK for the rules 1.4 changed or added — SEM-G-1..9 and SEM-A-1.
 *
 * Test-only, for the same reason `loadContractBundle` is: this package ships no runtime code, and
 * the one place the consumers' checkers (TypeScript in AUP, Go in AUE) can be held to the same
 * answers is the package that defines the rules. `test-vectors/index.json`'s `semantic_invalid`
 * set is that answer sheet.
 *
 * Iterative, with a visited set, never recursive: a hostile document may declare ten thousand
 * groups in one chain, and the answer must be SEM-G-9, not a stack overflow. Returns every fault
 * found as `{ rule, message }`, empty when the document is clean. Schema validation is assumed to
 * have passed already.
 */
export const GROUP_MAX_DEPTH = 32;

export function semanticFaults(document) {
  const faults = [];
  const fault = (rule, message) => faults.push({ rule, message });

  // Object index: kind, and the owning object (entity for a brush, brush for a face).
  const objects = new Map();
  for (const entity of document.entities ?? []) {
    objects.set(entity.entity_id, { kind: 'entity', parent: null });
    for (const item of entity.content ?? []) {
      if (item.kind !== 'brush') continue;
      objects.set(item.brush_id, { kind: 'brush', parent: entity.entity_id });
      for (const face of item.faces ?? []) objects.set(face.face_id, { kind: 'face', parent: item.brush_id });
    }
  }
  const groups = new Map();
  for (const group of document.groups ?? []) {
    if (groups.has(group.group_id)) fault('SEM-G-3', `group_id ${group.group_id} is declared twice`);
    else groups.set(group.group_id, group);
  }

  const idOf = (member) => member.entity_id ?? member.brush_id ?? member.face_id ?? member.group_id;
  const parentOf = new Map();   // direct-member id -> the one group that lists it
  for (const group of groups.values()) {
    for (const member of group.members) {
      const id = idOf(member);
      if (member.kind === 'group') {
        if (!groups.has(id)) fault('SEM-G-1', `${group.group_id} names group ${id}, which is not declared`);
        if (id === group.group_id) fault('SEM-G-8', `${id} is a member of itself`);
      } else {
        const object = objects.get(id);
        if (!object) fault('SEM-G-1', `${group.group_id} names ${member.kind} ${id}, which is not declared`);
        else if (object.kind !== member.kind) fault('SEM-G-2', `${id} is a ${object.kind}, not a ${member.kind}`);
      }
      if (parentOf.has(id)) fault('SEM-G-4', `${id} is a direct member of both ${parentOf.get(id)} and ${group.group_id}`);
      else parentOf.set(id, group.group_id);
    }
  }

  // SEM-G-8 (cycles) and SEM-G-9 (depth): walk each group's parent chain. Following `parentOf`
  // upward is iterative and memoized, so the whole pass is linear; a cycle is found by revisiting.
  const depthOf = new Map();
  const rootOf = new Map();
  for (const start of groups.keys()) {
    const chain = [];
    const seen = new Set();
    let at = start;
    let cyclic = false;
    while (at !== undefined && !depthOf.has(at)) {
      if (seen.has(at)) {
        cyclic = true;
        if (at !== start || chain.length > 1) fault('SEM-G-8', `the group graph has a cycle through ${at}`);
        break;
      }
      seen.add(at);
      chain.push(at);
      at = parentOf.get(at);
    }
    let depth = at !== undefined && depthOf.has(at) ? depthOf.get(at) : 0;
    const root = cyclic ? null : (at !== undefined ? rootOf.get(at) : chain[chain.length - 1]);
    for (let i = chain.length - 1; i >= 0; i -= 1) {
      depthOf.set(chain[i], ++depth);
      rootOf.set(chain[i], root);
    }
  }
  const deepest = Math.max(0, ...depthOf.values());
  if (deepest > GROUP_MAX_DEPTH)
    fault('SEM-G-9', `groups nest ${deepest} deep; at most ${GROUP_MAX_DEPTH} is allowed`);

  // SEM-G-5 across the nesting: the root of a group tree reaches every object in the tree, so an
  // object and one of its own ancestors (face -> brush -> entity) must not both be direct members
  // anywhere in ONE tree. Linear: compare the roots of the two direct parents.
  for (const [id, group] of parentOf) {
    if (!objects.has(id)) continue;
    for (let up = objects.get(id).parent; up; up = objects.get(up)?.parent) {
      const other = parentOf.get(up);
      if (other === undefined) continue;
      const [a, b] = [rootOf.get(group), rootOf.get(other)];
      if (a !== null && a !== undefined && a === b)
        fault('SEM-G-5', `${a} reaches both ${up} and its descendant ${id}`);
    }
  }

  // SEM-A-1: authorship text is Unicode — no unpaired surrogate survives parsing unnoticed.
  for (const [field, value] of Object.entries(document.authorship ?? {}))
    if (typeof value === 'string' && !value.isWellFormed())
      fault('SEM-A-1', `authorship.${field} contains an unpaired surrogate`);
  return faults;
}

export const describeErrors = (errors) =>
  (errors ?? []).map((error) => `${error.keyword} @ ${error.instancePath || '/'}: ${error.message}`).join('\n  ');

/**
 * `$MAPPER_ROOT`, or null. Only the generated corpora need it; the whole published contract is in
 * this repository and is tested from a bare clone.
 */
export const mapperRoot = () => {
  const candidates = [process.env.MAPPER_ROOT,
                      path.resolve(PACKAGE_ROOT, '../../../mapper'),
                      path.resolve(PACKAGE_ROOT, '../../../../mapper')];
  for (const candidate of candidates)
    if (candidate && fs.existsSync(path.join(candidate, 'LLM'))) return candidate;
  return null;
};

/** Every `.apmap` under `directory`, recursively. Empty when the directory does not exist. */
export const apmapFiles = (directory) =>
  fs.existsSync(directory)
    ? fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) =>
        entry.isDirectory()
          ? apmapFiles(path.join(directory, entry.name))
          : entry.name.endsWith('.apmap') ? [path.join(directory, entry.name)] : [])
    : [];
