/**
 * The staged, non-fail-fast APMap validation pipeline.
 *
 *     json-parse  →  schema  →  semantic  →  geometry
 *
 * A failure in one stage never blocks the others. Only `json-parse` can stop the pipeline, and only
 * because nothing downstream can run without a decoded value; every other stage runs to completion
 * and collects. Around all four sits one outer `try`/`catch`, so a defect in the loader itself
 * becomes a reported `generic` issue rather than an unhandled throw in a Web Worker.
 *
 * Stage ownership:
 *
 * - `json-parse` — the file as bytes: JSON syntax, the serialization rules (SER-1 … SER-9), and the
 *   lexical faults `JSON.parse` cannot describe. See `apmapScan.ts`.
 * - `schema` — the published contract: the ONE current schema in `@auto-pigeon/apmap-schema`,
 *   through `ajv` with `allErrors: true` so every violation is reported. The schema is *loaded*,
 *   never embedded: the browser fetches it from the backend, which reads the published file. When
 *   no schema was supplied the stage says so as a warning and the other stages still run — it never
 *   silently passes. When a schema WAS supplied and no validator for it can run (the browser's CSP
 *   forbids `ajv`'s compile, and a precompiled validator is matched by digest — `schemaValidator.ts`)
 *   the document is refused: `NEW_267`.
 * - `semantic` — SEM-1 … SEM-11, SEM-G-1 … SEM-G-9 and SEM-A-1 (`groupGraph.ts`), SEM-Q-1 … SEM-Q-5 (`quake3Surfaces.ts`), plus the Quake-domain faults no SEM rule covers (a dangling
 *   `target`, a duplicate `targetname`, a negative light) and the encoding faults that survive
 *   decoding (an embedded NUL, a right-to-left override, a two-megabyte value).
 * - `geometry` — render readiness: what will make the convex-winding kernel drop a face or draw a
 *   solid inside out. Distinguishable from `semantic` by consequence, not by document location.
 *
 * SEM-6 (a brush encloses a finite volume) and SEM-7 (a face contributes a facet) remain the
 * kernel's answer, as `docs/development/apmap.md` records; SEM-10 (a derived id equals its digest)
 * needs the producer's identity inputs and is not checkable from the document alone.
 */
import Ajv2020, { type ErrorObject, type ValidateFunction } from 'ajv/dist/2020.js';
import { ApmapIssues, MAX_APMAP_ISSUES, issueValue, pointer, type ApmapIssue } from '../issues.ts';
import { lineColumn, scanApmapText } from './scan.ts';
import { currentApmapVersion, isApmapReadable, unsupportedApmapMessage, type ApmapDocument } from './types.ts';
import { groupGraphFaults } from './groupGraph.ts';
import { quake3SurfaceFaults } from './quake3Surfaces.ts';
import { APMAP_AJV_OPTIONS, apmapValidatorGeneration, precompiledValidatorFor } from './schemaValidator.ts';

export type ApmapInspection = {
  /** The decoded document when one could be produced, whatever else is wrong with it. */
  value: Record<string, unknown> | null;
  issues: ApmapIssue[];
  /** True when either issue bound dropped something, so a reader knows the list is not exhaustive. */
  truncated: boolean;
  suppressed: number;
};

export type InspectOptions = {
  /** The CURRENT published JSON Schema. Absent means the stage reports itself unavailable, not that it passed. */
  schema?: unknown;
  /**
   * Every readable contract, keyed by version — the bundle the backend serves at
   * `/api/apmap/schema`. A document is measured against the schema MATCHING what it declares, so a
   * 1.0 document meets 1.0's contract and not the current one. Absent means current-only, which is
   * what a caller that has only ever been handed one schema honestly has.
   */
  schemas?: Record<string, unknown>;
  maxIssues?: number;
};

const isObject = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const asArray = (value: unknown): unknown[] => (Array.isArray(value) ? value : []);
const text = (value: unknown): string | null => (typeof value === 'string' ? value : null);
const RTL = /[\u202a-\u202e\u2066-\u2069]/;
const RTL_GLOBAL = /[\u202a-\u202e\u2066-\u2069]/g;
const NON_ASCII = /[^\x20-\x7e]/;
const MAX_STRING_BYTES = 64 * 1024;
/**
 * A value shaped like a stack trace or like markup. Nothing in a Quake entity legitimately looks
 * like this, and a value that renders as UI when a diagnostic quotes it is the fault, not the text.
 * Auto-Pigeon's Debug console sets text rather than HTML, so this reports the attempt; it is not
 * what makes the console safe.
 */
const INJECTION = /<\/?\s*(script|iframe|object|embed|img|svg|pre)\b|Traceback \(most recent call last\)|javascript:/i;
/** Beyond this a coordinate is outside any sane Quake map; `qbsp`'s own world bound is ±4096. */
const COORDINATE_LIMIT = 1e6;
const INT32_LIMIT = 2 ** 31;
const MAX_PAYLOAD_DEPTH = 32;

// ---- stage 1: json-parse ----------------------------------------------------------------

function jsonParseStage(source: string, issues: ApmapIssues): { value: unknown; parsed: boolean } {
  scanApmapText(source, issues);
  try {
    return { value: JSON.parse(source), parsed: true };
  } catch (error) {
    const message = (error as Error).message ?? String(error);
    const position = /position (\d+)/.exec(message);
    const place = position ? ` (${(({ line, column }) => `line ${line} column ${column}`)(lineColumn(source, Number(position[1])))})` : '';
    issues.add({ stage: 'json-parse', severity: 'fatal', message: `The APMap document is not valid JSON: ${message}${place}`,
                 path: null, value: position ? issueValue(source.slice(Math.max(0, Number(position[1]) - 30), Number(position[1]) + 30)) : null });
    return { value: null, parsed: false };
  }
}

// ---- stage 2: schema --------------------------------------------------------------------

/**
 * One validator per schema OBJECT. Obtaining the APMap validator is not cheap.
 *
 * A map rather than a single slot, because with backward reads a session alternates between two
 * contracts — open a 1.0 prefab, open a 1.1 map, open another 1.0 prefab — and a one-entry cache
 * would recompile on every switch while looking like it was caching. Keyed by identity on a
 * `WeakMap`, so an entry lives exactly as long as the schema the caller is holding.
 *
 * A validator that cannot be obtained is cached too, as its refusal — and the refusal is reported
 * for EVERY document, not once per schema. `NEW_267`: this used to be a warning reported once, so a
 * browser under AUP's CSP (where `ajv`'s compile always throws) validated nothing and said so only
 * about the first document it saw. A document nobody could check is refused, each time.
 */
type Obtained = { validate: ValidateFunction } | { refusal: string };
let compiled = new WeakMap<object, { generation: number; obtained: Obtained }>();

function validatorFor(schema: unknown, version: string | undefined): Obtained {
  const key = schema as object;
  const cached = compiled.get(key);
  if (cached !== undefined && cached.generation === apmapValidatorGeneration()) return cached.obtained;
  // A host that ships precompiled validators (the browser) never compiles: see `schemaValidator.ts`.
  let obtained: Obtained | undefined = precompiledValidatorFor(schema, version);
  if (!obtained) {
    try {
      obtained = { validate: new Ajv2020({ ...APMAP_AJV_OPTIONS }).compile(key) };
    } catch (error) {
      obtained = { refusal: `The APMap schema could not be compiled, so this document cannot be checked against its contract and is refused: ${(error as Error).message}` };
    }
  }
  compiled.set(key, { generation: apmapValidatorGeneration(), obtained });
  return obtained;
}

/** Only tests that swap the schema need this. */
export const forgetApmapValidator = (): void => { compiled = new WeakMap(); };

function woundForSchemaError(error: ErrorObject): string | undefined {
  const path = error.instancePath, data = (error as ErrorObject & { data?: unknown }).data;
  const inPoints = /\/plane\/points\//.test(path);
  switch (error.keyword) {
    case 'required': {
      const missing = String((error.params as { missingProperty?: string }).missingProperty ?? '');
      if (missing === 'apmap_version') return 'schema:missing-version-field';
      if (missing === 'entity_id') return 'refs:missing-entity-id';
      if (missing === 'texture') return 'geometry:missing-texture-field';
      if (missing === 'projection') return 'geometry:missing-projection';
      return 'schema:missing-required-field';
    }
    case 'additionalProperties': return 'schema:unexpected-extra-field';
    case 'enum': return 'schema:enum-value-invalid';
    case 'const': return path === '/apmap_version' ? 'schema:unknown-version-string' : 'schema:enum-value-invalid';
    case 'minItems': return /\/faces$/.test(path) ? 'geometry:brush-too-few-faces' : 'schema:empty-required-array';
    case 'minLength': return /\/texture$/.test(path) ? 'geometry:empty-texture-name' : undefined;
    case 'maxItems': return undefined;
    case 'pattern':
      if (/\/texture$/.test(path)) return 'geometry:whitespace-in-texture-name';
      if (/\/relationships\/\d+\/relationship_id$/.test(path)) return 'refs:relationship-id-malformed';
      if (/\/extensions\//.test(path)) return 'sneaky:wound-in-rarely-touched-optional-field';
      return /_id$/.test(path) ? 'schema:id-pattern-violation' : undefined;
    // A member of `extensions` must be a reverse-DNS namespace. A bare key there is a fault off the
    // happy path, which is exactly why it is in the catalog's `sneaky` class.
    case 'propertyNames': return /\/extensions$/.test(path) ? 'sneaky:wound-in-rarely-touched-optional-field' : undefined;
    case 'minimum': return /\/revision$/.test(path) ? 'numeric:negative-in-unsigned-field' : undefined;
    case 'oneOf': return /\/content\/\d+$/.test(path) ? 'schema:wrong-nesting-depth' : undefined;
    case 'type': {
      const expected = String((error.params as { type?: string | string[] }).type ?? '');
      if (data === null) return 'schema:null-for-non-nullable';
      if (expected === 'integer' && typeof data === 'number') return 'numeric:fractional-integer-field';
      if (expected === 'object' && Array.isArray(data)) return 'schema:array-where-object-expected';
      if (expected === 'array' && isObject(data)) return 'schema:object-where-array-expected';
      // Only an *object* id: `/document_id` is the document's own name, not a reference to one.
      if (expected === 'string' && typeof data === 'number') return /\/(entity|brush|face)_id$/.test(path) ? 'refs:mismatched-id-types' : 'schema:wrong-type-number-for-string';
      if ((expected === 'number' || expected === 'integer') && typeof data === 'string') {
        if (data === 'NaN') return 'numeric:nan-string-coordinate';
        if (data === 'Infinity' || data === '-Infinity') return 'numeric:infinity-string-coordinate';
        if (/^[-+]?0x/i.test(data)) return 'numeric:hex-string-numeric-field';
        return inPoints ? 'geometry:coordinates-as-strings' : 'schema:wrong-type-string-for-number';
      }
      return undefined;
    }
    default: return undefined;
  }
}

function schemaStage(value: unknown, options: InspectOptions, issues: ApmapIssues): void {
  const declared = isObject(value) ? value.apmap_version : undefined;
  let readable = false;
  if (typeof declared !== 'string' || !declared)
    issues.add({ stage: 'schema', severity: 'fatal', message: 'The document declares no apmap_version, so it names no contract to be read against',
                 path: '/apmap_version', value: issueValue(declared ?? null), woundId: 'schema:missing-version-field' });
  else if (!isApmapReadable(declared))
    issues.add({ stage: 'schema', severity: 'fatal', message: unsupportedApmapMessage(declared),
                 path: '/apmap_version', value: declared, woundId: 'schema:unknown-version-string' });
  else readable = true;

  // Version-matched. `schemas` is the bundle; `schema` is the current contract, which is the right
  // answer only for a document that declares the current version. A readable legacy document with
  // no legacy schema on hand is reported UNAVAILABLE rather than measured against the current
  // contract: 1.1 accepts strictly more than 1.0, so that substitution would quietly pass a
  // document nobody checked, which is the one outcome this stage exists to prevent.
  // A document that names no readable version has already been refused above, with the reason; it
  // has no contract to be measured against, so there is nothing "unavailable" to add (`NEW_267`: the
  // editor used to follow the refusal with "the published schema is unavailable", which is false).
  if (!readable) return;
  const version = declared as string;
  const schema = options.schemas?.[version] ?? (version === currentApmapVersion() ? options.schema : undefined);
  if (schema === undefined || schema === null) {
    issues.add({ stage: 'schema', severity: 'warning',
                 message: version === currentApmapVersion()
                   ? 'The published APMap schema is unavailable, so schema validation did not run; the other stages did'
                   : `The published APMap ${version} schema is unavailable, so schema validation did not run against the contract this document declares; the other stages did`,
                 path: null, value: null });
    return;
  }
  const obtained = validatorFor(schema, version);
  if ('refusal' in obtained) {
    issues.add({ stage: 'schema', severity: 'fatal', message: obtained.refusal, path: null, value: null });
    return;
  }
  const validate = obtained.validate;
  if (validate(value)) return;
  for (const error of validate.errors ?? []) {
    const path = error.instancePath || '';
    issues.add({ stage: 'schema', severity: 'error', message: `${path || 'the document'} ${error.message ?? 'failed schema validation'}`,
                 path: path || null, value: issueValue((error as ErrorObject & { data?: unknown }).data),
                 woundId: woundForSchemaError(error) }, `schema:${error.keyword}:${error.schemaPath}`);
  }
}

// ---- a defensive view of the document ---------------------------------------------------

type FaceView = { path: string; id: string | null; value: Record<string, unknown> };
type BrushView = { path: string; id: string | null; value: Record<string, unknown>; faces: FaceView[] };
type EntityView = { path: string; index: number; id: string | null; value: Record<string, unknown>;
                    properties: { key: string; value: unknown; path: string }[]; brushes: BrushView[] };

/** Nothing below is trusted to have the type the schema requires: a wounded document is the input. */
function entityViews(document: Record<string, unknown>): EntityView[] {
  const views: EntityView[] = [];
  asArray(document.entities).forEach((entity, index) => {
    if (!isObject(entity)) return;
    const path = `/entities/${index}`;
    const view: EntityView = { path, index, id: text(entity.entity_id), value: entity, properties: [], brushes: [] };
    asArray(entity.content).forEach((item, position) => {
      if (!isObject(item)) return;
      const itemPath = `${path}/content/${position}`;
      if (item.kind === 'brush') {
        const brush: BrushView = { path: itemPath, id: text(item.brush_id), value: item, faces: [] };
        asArray(item.faces).forEach((face, faceIndex) => {
          if (isObject(face)) brush.faces.push({ path: `${itemPath}/faces/${faceIndex}`, id: text(face.face_id), value: face });
        });
        view.brushes.push(brush);
      } else if (item.kind === 'property') {
        view.properties.push({ key: String(item.key ?? ''), value: item.value, path: itemPath });
      }
    });
    views.push(view);
  });
  return views;
}

const propertyOf = (entity: EntityView, key: string): string | null => {
  for (let index = entity.properties.length - 1; index >= 0; index -= 1)
    if (entity.properties[index].key === key) return text(entity.properties[index].value);
  return null;
};

// ---- stage 3: semantic ------------------------------------------------------------------

/** SEM-11 over the decoded tree: `1e400` is ordinary JSON that decodes to Infinity. */
function nonFinite(value: unknown, path: string, issues: ApmapIssues, depth = 0): void {
  if (depth > 12) return;
  if (typeof value === 'number') {
    if (!Number.isFinite(value))
      issues.add({ stage: 'semantic', severity: 'error', message: `SEM-11: ${path} decodes to ${value}, which is not a finite number`, path, value: String(value),
                   woundId: 'numeric:scientific-notation-coordinate' }, 'semantic:non-finite');
    return;
  }
  if (Array.isArray(value)) { value.forEach((item, index) => nonFinite(item, pointer(path, index), issues, depth + 1)); return; }
  if (isObject(value)) for (const [key, item] of Object.entries(value)) nonFinite(item, pointer(path, key), issues, depth + 1);
}

const depthOf = (value: unknown, depth = 0): number => {
  if (depth > 512) return depth;
  if (Array.isArray(value)) return value.reduce<number>((deepest, item) => Math.max(deepest, depthOf(item, depth + 1)), depth);
  if (isObject(value)) return Object.values(value).reduce<number>((deepest, item) => Math.max(deepest, depthOf(item, depth + 1)), depth);
  return depth;
};

function encodingChecks(label: string, value: string, path: string, issues: ApmapIssues): void {
  if (value.includes('\0'))
    issues.add({ stage: 'semantic', severity: 'error', message: `The ${label} contains an embedded NUL, which truncates a C-string consumer`, path, value: issueValue(value.replace(/\0/g, '\\u0000')), woundId: 'encoding:embedded-null-byte' }, 'semantic:nul');
  if (RTL.test(value))
    issues.add({ stage: 'semantic', severity: 'error', message: `The ${label} contains a Unicode bidirectional override, so what it displays as is not what it is`, path, value: issueValue(value.replace(RTL_GLOBAL, '[bidi]')), woundId: 'encoding:rtl-override-in-texture-name' }, 'semantic:rtl');
  if (INJECTION.test(value))
    issues.add({ stage: 'semantic', severity: 'warning', message: `The ${label} is shaped like a stack trace or like markup, not like Quake data`, path, value: issueValue(value.slice(0, 120)), woundId: 'sneaky:error-message-injection-string' }, 'semantic:injection');
  if (value.length > MAX_STRING_BYTES)
    issues.add({ stage: 'semantic', severity: 'warning', message: `The ${label} is ${value.length} characters long`, path, value: issueValue(value.slice(0, 80)), woundId: 'encoding:oversized-string-value' }, 'semantic:oversized');
}

function semanticStage(document: Record<string, unknown>, views: EntityView[], issues: ApmapIssues): void {
  nonFinite(document, '', issues);

  // SEM-1: one id, one object. The wound catalog splits the same rule by object kind.
  const declared = new Set<string>();
  const seen = new Map<string, string>();
  const claim = (kind: string, id: string | null, path: string, wound: string) => {
    if (!id) return;
    declared.add(id);
    if (seen.has(id))
      issues.add({ stage: 'semantic', severity: 'error', message: `SEM-1: the ${kind} id ${id} is declared more than once`, path, value: id, woundId: wound }, `semantic:duplicate-id:${kind}`);
    else seen.set(id, kind);
  };
  const policy = document.id_policy;
  for (const entity of views) {
    claim('entity', entity.id, `${entity.path}/entity_id`, 'refs:duplicate-entity-id');
    if (policy === 'derived' && !('derived_from' in entity.value))
      issues.add({ stage: 'semantic', severity: 'warning', message: `SEM-8: entity ${entity.id ?? entity.index} has no derived_from under id_policy 'derived'`, path: entity.path, value: null }, 'semantic:sem8');
    for (const brush of entity.brushes) {
      claim('brush', brush.id, `${brush.path}/brush_id`, 'geometry:duplicate-brush-id');
      for (const face of brush.faces) {
        claim('face', face.id, `${face.path}/face_id`, 'refs:duplicate-entity-id');
        const synthetic = face.value.synthetic, reason = face.value.synthetic_reason;
        if ((synthetic !== undefined) !== (reason !== undefined))
          issues.add({ stage: 'semantic', severity: 'error', message: `SEM-3: face ${face.id ?? ''} declares only one of synthetic / synthetic_reason`, path: face.path, value: issueValue({ synthetic, synthetic_reason: reason }) }, 'semantic:sem3');
        else if (synthetic !== undefined && synthetic !== true)
          issues.add({ stage: 'semantic', severity: 'error', message: `SEM-3: face ${face.id ?? ''} declares a synthetic flag that is not true`, path: `${face.path}/synthetic`, value: issueValue(synthetic) }, 'semantic:sem3');
        const texture = text(face.value.texture);
        if (texture !== null) encodingChecks('texture name', texture, `${face.path}/texture`, issues);
      }
    }
    for (const property of entity.properties) {
      const value = text(property.value);
      if (value !== null) encodingChecks(`property ${property.key}`, value, `${property.path}/value`, issues);
    }
  }

  // SEM-2 and the reference wounds around it.
  const relationships = asArray(document.relationships);
  const edges = new Map<string, Set<string>>();
  relationships.forEach((relationship, index) => {
    if (!isObject(relationship)) return;
    const path = `/relationships/${index}`;
    for (const end of ['from', 'to'] as const) {
      const endpoint = text(relationship[end]);
      if (endpoint && !declared.has(endpoint))
        issues.add({ stage: 'semantic', severity: 'error', message: `SEM-2: relationship ${end} ${endpoint} is declared nowhere in this document`, path: `${path}/${end}`, value: endpoint, woundId: 'refs:relationship-target-nonexistent' }, 'semantic:sem2');
    }
    const from = text(relationship.from), to = text(relationship.to);
    if (from && to) { if (!edges.has(from)) edges.set(from, new Set()); edges.get(from)!.add(to); }
    if (depthOf(relationship.payload) > MAX_PAYLOAD_DEPTH)
      issues.add({ stage: 'semantic', severity: 'warning', message: `The relationship payload nests more than ${MAX_PAYLOAD_DEPTH} levels deep, which only manifests once relationships are resolved`, path: `${path}/payload`, value: `depth ${depthOf(relationship.payload)}`, woundId: 'sneaky:manifests-on-second-parse-only' }, 'semantic:payload-depth');
  });
  for (const [from, targets] of edges)
    for (const to of targets)
      if (edges.get(to)?.has(from))
        issues.add({ stage: 'semantic', severity: 'warning', message: `Relationships form a cycle between ${from} and ${to}`, path: '/relationships', value: `${from} ↔ ${to}`, woundId: 'refs:circular-relationship' }, 'semantic:cycle');

  // ---- APMap 1.1: the two additions, checked in code as well as against the schema.
  //
  // The schema stage already covers both when a 1.1 schema was supplied — but it is *supplied*, and
  // when it is not, that stage reports itself unavailable and checks nothing. These are the two 1.1
  // faults a consumer would otherwise act on unchecked: a `derived_from` that names no cause, and a
  // `broken` flag that is not a boolean and therefore has no defined meaning at all.
  const KNOWN_DERIVED_FROM_KINDS = ['source_map', 'package', 'authored', 'synthetic', 'operation'];
  const checkDerivedFrom = (value: unknown, path: string): void => {
    if (!isObject(value)) return;
    const kind = text(value.kind);
    if (kind === null || !KNOWN_DERIVED_FROM_KINDS.includes(kind)) {
      issues.add({ stage: 'semantic', severity: 'error', path: `${path}/kind`, value: issueValue(value.kind),
                   message: `derived_from names kind ${JSON.stringify(value.kind)}, which is none of the five APMap defines (${KNOWN_DERIVED_FROM_KINDS.join(', ')}); a new provenance kind is a schema change, never a producer's invention` },
                 'semantic:derived-from-kind');
      return;
    }
    if (kind !== 'operation') return;
    const operationId = text(value.operation_id);
    if (!operationId)
      issues.add({ stage: 'semantic', severity: 'error', path: `${path}/operation_id`, value: issueValue(value.operation_id),
                   message: "derived_from kind 'operation' carries no operation_id; it is the one member beyond kind that is always required, and without it the record names no cause" },
                 'semantic:operation-id');
    // `replaces` is deliberately *not* checked against the declared ids. It names the objects this
    // one supersedes, which by construction are no longer in the document — treating them as
    // dangling references would fire on every correct 1.1 document that records a replacement.
  };

  for (const entity of views) {
    checkDerivedFrom(entity.value.derived_from, `${entity.path}/derived_from`);
    for (const brush of entity.brushes) {
      checkDerivedFrom(brush.value.derived_from, `${brush.path}/derived_from`);
      const broken = brush.value.broken;
      if (broken !== undefined && typeof broken !== 'boolean')
        issues.add({ stage: 'semantic', severity: 'error', path: `${brush.path}/broken`, value: issueValue(broken),
                     message: `brush ${brush.id ?? ''} declares broken as ${typeof broken}, not a boolean; there are two states and absence is the third spelling of false` },
                   'semantic:broken-type');
      // Nothing reacts to `broken` being *true*. It is not a validation result and it is not a
      // gate: a level with broken brushes in it is ordinary work in progress.
      for (const face of brush.faces) checkDerivedFrom(face.value.derived_from, `${face.path}/derived_from`);
    }
  }

  // A synthetic face must be traceable to something that exists.
  for (const entity of views)
    for (const brush of entity.brushes)
      for (const face of brush.faces) {
        const source = isObject(face.value.derived_from) ? text((face.value.derived_from as Record<string, unknown>).source_object) : null;
        if (source && !declared.has(source))
          issues.add({ stage: 'semantic', severity: 'error', message: `The synthetic face ${face.id ?? ''} names source_object ${source}, which is declared nowhere in this document`, path: `${face.path}/derived_from/source_object`, value: source, woundId: 'refs:dangling-synthetic-source-object' }, 'semantic:synthetic-source');
      }

  // SEM-4: a source-world document has nothing to transform back to source world.
  const frame = isObject(document.frame) ? document.frame : null;
  const toSource = frame && isObject(frame.to_source) ? frame.to_source : null;
  if (frame?.space === 'source_world' && toSource && toSource.type !== 'identity')
    issues.add({ stage: 'semantic', severity: 'error', message: `SEM-4: a source_world document must declare to_source type 'identity', not ${String(toSource.type)}`, path: '/frame/to_source/type', value: issueValue(toSource.type) });
  if (toSource && toSource.type === 'affine')
    issues.add({ stage: 'semantic', severity: 'error', message: "APMap 1.0 reserves the transform type 'affine' and rejects it; this build does not guess at a reserved name", path: '/frame/to_source/type', value: 'affine', woundId: 'sneaky:reserved-future-field-name' });

  // ---- Quake-domain rules. No SEM-* rule covers these; a renderer still has to notice them.
  const classnames = views.map((entity) => propertyOf(entity, 'classname'));
  if (document.role === 'full_map') {
    const worldspawns = classnames.filter((name) => name === 'worldspawn').length;
    if (worldspawns === 0)
      issues.add({ stage: 'semantic', severity: 'error', message: 'SEM-9: a full_map document declares no worldspawn entity', path: '/entities', value: null, woundId: 'entity:missing-worldspawn' });
    else if (worldspawns > 1)
      issues.add({ stage: 'semantic', severity: 'error', message: `SEM-9: a full_map document declares ${worldspawns} worldspawn entities; exactly one is allowed`, path: '/entities', value: worldspawns, woundId: 'entity:duplicate-worldspawn' });
    else if (classnames[0] !== 'worldspawn')
      issues.add({ stage: 'semantic', severity: 'error', message: `SEM-9: worldspawn must be the first entity, but the first is ${classnames[0] ?? 'unnamed'}`, path: '/entities/0', value: issueValue(classnames[0]), woundId: 'entity:worldspawn-not-first' });
    if (!classnames.includes('info_player_start'))
      issues.add({ stage: 'semantic', severity: 'warning', message: 'The map declares no info_player_start, so it cannot be spawned into', path: '/entities', value: null, woundId: 'entity:no-player-start' });
  }

  const targetnames = new Map<string, number>();
  for (const entity of views) {
    const name = propertyOf(entity, 'targetname');
    if (name) targetnames.set(name, (targetnames.get(name) ?? 0) + 1);
  }
  // No duplicate-targetname check. `entity:duplicate-targetname` is in the wound catalog, but two
  // entities sharing a targetname is ordinary Quake — it is how a pair of doors opens together, and
  // e1m1 alone declares `t3` six times. A diagnostic that fires on id Software's own maps is noise,
  // and noise in Debug is worse than a gap in it. The wound is reported as uncovered instead.

  const brushEntities = views.filter((entity) => entity.brushes.length > 0).length;
  for (const entity of views) {
    const classname = propertyOf(entity, 'classname');
    const declaredClassname = entity.properties.some((property) => property.key === 'classname');
    if (!declaredClassname)
      issues.add({ stage: 'semantic', severity: 'error', message: `Entity ${entity.index} declares no classname`, path: entity.path, value: null, woundId: 'entity:missing-classname' }, 'semantic:missing-classname');
    else if (classname === '')
      issues.add({ stage: 'semantic', severity: 'error', message: `Entity ${entity.index} declares an empty classname`, path: entity.path, value: '', woundId: 'entity:empty-classname' }, 'semantic:empty-classname');
    else if (classname && NON_ASCII.test(classname))
      issues.add({ stage: 'semantic', severity: 'warning', message: `Entity ${entity.index} has a non-ASCII classname, which no Quake engine will match`, path: entity.path, value: issueValue(classname), woundId: 'encoding:emoji-in-classname' }, 'semantic:classname-non-ascii');

    for (const reference of ['target', 'killtarget'] as const) {
      const value = propertyOf(entity, reference);
      if (value && !targetnames.has(value))
        issues.add({ stage: 'semantic', severity: 'warning', message: `Entity ${entity.index} (${classname ?? 'unnamed'}) names ${reference} ${value}, which no entity declares as its targetname`, path: entity.path, value, woundId: 'entity:dangling-target-reference' }, 'semantic:dangling-target');
    }
    const angle = propertyOf(entity, 'angle');
    if (angle !== null && angle !== '' && Number.isFinite(Number(angle)) && Math.abs(Number(angle)) > 360)
      issues.add({ stage: 'semantic', severity: 'warning', message: `Entity ${entity.index} declares angle ${angle}, outside the -360…360 a Quake angle occupies`, path: entity.path, value: angle, woundId: 'entity:angle-out-of-range' }, 'semantic:angle');
    const spawnflags = propertyOf(entity, 'spawnflags');
    if (spawnflags !== null && !/^-?\d+$/.test(spawnflags.trim()))
      issues.add({ stage: 'semantic', severity: 'warning', message: `Entity ${entity.index} declares non-integer spawnflags ${JSON.stringify(spawnflags)}`, path: entity.path, value: issueValue(spawnflags), woundId: 'entity:spawnflags-non-integer' }, 'semantic:spawnflags');
    const light = propertyOf(entity, 'light');
    if (light !== null && Number.isFinite(Number(light)) && Number(light) < 0)
      issues.add({ stage: 'semantic', severity: 'warning', message: `Entity ${entity.index} declares a negative light value ${light}`, path: entity.path, value: light, woundId: 'entity:negative-light-value' }, 'semantic:negative-light');
    for (const numeric of ['origin', 'angle', 'angles', 'light', 'spawnflags'] as const) {
      const value = propertyOf(entity, numeric);
      if (value !== null && value.trim() === '')
        issues.add({ stage: 'semantic', severity: 'warning', message: `Entity ${entity.index} declares ${numeric} as an empty string rather than a number`, path: entity.path, value: '', woundId: 'entity:numeric-field-as-empty-string' }, 'semantic:empty-numeric');
    }
    const model = propertyOf(entity, 'model');
    const submodel = model && /^\*(\d+)$/.exec(model);
    if (submodel && Number(submodel[1]) >= brushEntities)
      issues.add({ stage: 'semantic', severity: 'warning', message: `Entity ${entity.index} references submodel ${model}, but the document declares ${brushEntities} brush entities`, path: entity.path, value: model, woundId: 'entity:model-reference-invalid-index' }, 'semantic:submodel');
    const lower = new Map<string, string>();
    for (const property of entity.properties) {
      const key = property.key.toLowerCase(), previous = lower.get(key);
      if (previous !== undefined && previous !== property.key)
        issues.add({ stage: 'semantic', severity: 'warning', message: `Entity ${entity.index} declares both ${previous} and ${property.key}, which differ only in case`, path: property.path, value: property.key, woundId: 'entity:duplicate-key-different-casing' }, 'semantic:key-casing');
      lower.set(key, property.key);
    }
  }

  // SEM-G-1 … SEM-G-9 and SEM-A-1 — the group graph and authorship, which only the whole document
  // can answer. One implementation, `groupGraph.ts`, shared with the editor's group operations.
  if (Array.isArray(document.groups) || isObject(document.authorship))
    for (const fault of groupGraphFaults(document as unknown as ApmapDocument))
      issues.add({ stage: 'semantic', severity: 'error', message: fault.message, path: fault.path, value: fault.rule }, `semantic:${fault.rule}`);

  // SEM-Q-1 … SEM-Q-5 — APMap 1.5's Quake III surfaces (`quake3Surfaces.ts`, `Q3_001`).
  for (const fault of quake3SurfaceFaults(document))
    issues.add({ stage: 'semantic', severity: 'error', message: fault.message, path: fault.path, value: fault.rule }, `semantic:${fault.rule}`);
}

// ---- stage 4: geometry ------------------------------------------------------------------

const subtract = (a: number[], b: number[]) => [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
const cross = (a: number[], b: number[]) => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];
const dot = (a: number[], b: number[]) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
const numbers = (value: unknown): number[] | null =>
  Array.isArray(value) && value.length === 3 && value.every((item) => typeof item === 'number' && Number.isFinite(item)) ? (value as number[]) : null;

function geometryStage(views: EntityView[], issues: ApmapIssues): void {
  /** Each brush's orientation, so a solid built inside out stands out against its neighbours. */
  const orientation: { path: string; id: string | null; sign: number }[] = [];
  for (const entity of views)
    for (const brush of entity.brushes) {
      if (brush.faces.length > 0 && brush.faces.length < 4)
        issues.add({ stage: 'geometry', severity: 'error', message: `Brush ${brush.id ?? ''} has ${brush.faces.length} faces; a closed convex solid needs at least four`, path: `${brush.path}/faces`, value: brush.faces.length, woundId: 'geometry:brush-too-few-faces' }, 'geometry:too-few-faces');
      const planes: { normal: number[]; origin: number[] }[] = [];
      const all: number[][] = [];
      for (const face of brush.faces) {
        const plane = isObject(face.value.plane) ? face.value.plane : null;
        const points = asArray(plane?.points).map(numbers);
        if (points.length !== 3 || points.some((point) => point === null)) continue;
        const [a, b, c] = points as number[][];
        all.push(a, b, c);
        for (const [index, point] of [a, b, c].entries())
          for (const [axis, value] of point.entries()) {
            const path = `${face.path}/plane/points/${index}/${axis}`;
            // Both can be true of one coordinate, and they are different faults: one is a map that
            // cannot be built, the other is a number a 32-bit consumer silently mangles.
            if (Math.abs(value) >= INT32_LIMIT)
              issues.add({ stage: 'geometry', severity: 'error', message: `A plane point coordinate is ${value}, past 2^31 — it overflows a 32-bit consumer`, path, value, woundId: 'numeric:int32-overflow' }, 'geometry:int32');
            if (Math.abs(value) > COORDINATE_LIMIT)
              issues.add({ stage: 'geometry', severity: 'error', message: `A plane point coordinate is ${value}, far outside any sane map bounds`, path, value, woundId: 'geometry:coordinates-out-of-bounds' }, 'geometry:out-of-bounds');
            if (Object.is(value, -0))
              issues.add({ stage: 'geometry', severity: 'warning', message: 'A plane point coordinate is negative zero; SER-7 collapses it to 0', path, value: '-0', woundId: 'numeric:negative-zero-coordinate' }, 'geometry:negative-zero');
          }
        const identical = [a, b, c].every((point) => point.every((value, axis) => value === a[axis]));
        const normal = cross(subtract(b, a), subtract(c, a));
        const area = Math.hypot(...normal);
        if (identical)
          issues.add({ stage: 'geometry', severity: 'error', message: `Face ${face.id ?? ''} declares three identical plane points, so it defines no plane`, path: `${face.path}/plane/points`, value: issueValue(a), woundId: 'geometry:zero-area-plane' }, 'geometry:zero-area');
        else if (area <= 1e-9)
          issues.add({ stage: 'geometry', severity: 'error', message: `SEM-5: face ${face.id ?? ''} declares three collinear plane points, so it defines no plane`, path: `${face.path}/plane/points`, value: issueValue([a, b, c]), woundId: 'geometry:collinear-plane-points' }, 'geometry:collinear');
        else planes.push({ normal: normal.map((value) => value / area), origin: a });

        const projection = isObject(face.value.projection) ? face.value.projection : null;
        const scale = Array.isArray(projection?.scale) ? projection.scale : null;
        if (scale) {
          for (const [axis, value] of scale.entries()) {
            const path = `${face.path}/projection/scale/${axis}`;
            if (value === 0)
              issues.add({ stage: 'geometry', severity: 'error', message: `Face ${face.id ?? ''} declares a texture scale of zero, which divides by zero in every UV consumer`, path, value: 0, woundId: 'geometry:zero-texture-scale' }, 'geometry:zero-scale');
            // No negative-scale check, for the same reason: a negative texture scale mirrors the
            // texture, which is a normal authoring choice and appears throughout the id maps.
          }
        }
        const rotation = projection?.rotation;
        if (rotation !== undefined && (typeof rotation !== 'number' || !Number.isFinite(rotation)))
          issues.add({ stage: 'geometry', severity: 'error', message: `Face ${face.id ?? ''} declares a texture rotation that is not a finite number`, path: `${face.path}/projection/rotation`, value: issueValue(rotation), woundId: 'geometry:nan-texture-rotation' }, 'geometry:rotation');
      }
      if (planes.length >= 4) {
        const centre = all.reduce((sum, point) => [sum[0] + point[0], sum[1] + point[1], sum[2] + point[2]], [0, 0, 0]).map((value) => value / all.length);
        // For a convex solid the centre lies on one side of every one of its planes. Which side is
        // a winding convention; that every brush in one document agrees is not.
        const signs = planes.map((plane) => Math.sign(dot(plane.normal, subtract(centre, plane.origin))));
        const inside = signs.filter((sign) => sign < 0).length, outside = signs.filter((sign) => sign > 0).length;
        if (inside === 0 || outside === 0) orientation.push({ path: brush.path, id: brush.id, sign: inside === 0 ? 1 : -1 });
      }
    }
  const negative = orientation.filter((brush) => brush.sign < 0).length;
  const dominant = negative >= orientation.length - negative ? -1 : 1;
  if (orientation.length >= 4)
    for (const brush of orientation)
      if (brush.sign !== dominant)
        issues.add({ stage: 'geometry', severity: 'warning', message: `Brush ${brush.id ?? ''} is wound opposite to every other brush in this document — schema-valid, and a solid turned inside out`, path: brush.path, value: null, woundId: 'sneaky:valid-schema-invalid-render' }, 'geometry:inverted');
}

// ---- the pipeline -----------------------------------------------------------------------

/**
 * Run every stage over one APMap document and return what was found. Never throws: a defect in this
 * pipeline is reported as a `generic` issue, because a throw inside a Web Worker is an anonymous
 * failure with nothing in it a reviewer can act on.
 */
export function inspectApmapText(source: string, options: InspectOptions = {}): ApmapInspection {
  const issues = new ApmapIssues(options.maxIssues ?? MAX_APMAP_ISSUES);
  let value: Record<string, unknown> | null = null;
  try {
    const parsed = jsonParseStage(source, issues);
    if (parsed.parsed) {
      if (!isObject(parsed.value)) {
        issues.add({ stage: 'json-parse', severity: 'fatal', message: 'An APMap document must be a JSON object', path: '', value: issueValue(Array.isArray(parsed.value) ? 'an array' : typeof parsed.value),
                     woundId: Array.isArray(parsed.value) ? 'structural:wrapped-in-array' : undefined });
      } else {
        value = parsed.value;
        const views = entityViews(value);
        for (const stage of [() => schemaStage(value, options, issues), () => semanticStage(value!, views, issues), () => geometryStage(views, issues)]) {
          // One stage failing must not take the others down with it.
          try { stage(); } catch (error) { genericIssue(error, issues); }
        }
      }
    }
  } catch (error) {
    genericIssue(error, issues);
  }
  return { value, issues: issues.items, truncated: issues.truncated, suppressed: issues.suppressed };
}

/** The last resort. `path` and `value` are best effort and `null` when genuinely unresolvable. */
function genericIssue(error: unknown, issues: ApmapIssues): void {
  const detail = error instanceof Error ? `${error.name}: ${error.message}` : typeof error === 'string' ? error : null;
  issues.add({ stage: 'generic', severity: 'fatal', message: 'Generic error', path: null, value: detail === null ? null : issueValue(detail) });
}

/**
 * **What stops a document from being WRITTEN** — `NEW_267`.
 *
 * Rendering tolerates a document that breaks the contract (a reader should be able to open it and
 * see what is wrong); a writer must not produce one. The contract is the published schema plus the
 * normative `SEM-*` rules of `SEMANTICS.md` — the same two things the backend and AUE enforce — so
 * this is: every fatal issue (a validator that could not run is one), every schema-stage error, and
 * every issue that cites a `SEM-` rule, in whichever stage found it (SEM-5 is geometry's). The
 * Quake-domain advice (a dangling `target`, a negative light) is not the contract and stays advice.
 */
export const apmapContractViolations = (inspection: ApmapInspection): ApmapIssue[] =>
  inspection.issues.filter((issue) => issue.severity === 'fatal'
    || (issue.severity === 'error' && (issue.stage === 'schema' || /^SEM-/.test(issue.message))));

/** Only the fatal ones stop a document from being rendered. */
export const apmapIsRenderable = (inspection: ApmapInspection): boolean =>
  inspection.value !== null && !inspection.issues.some((issue) => issue.severity === 'fatal');
