/**
 * 1.4's additions — authorship and nested groups — and the promotion that carries every legacy
 * corpus into the current contract (1.5, whose Quake III additions have their own file,
 * quake3.test.mjs). Tests only; nothing here is a runtime surface.
 */
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import {
  compile, compileDef, currentVersion, describeErrors, loadCurrentSchema, promoteToCurrent, readJson,
  semanticFaults, vector, vectorIndex, DEPRECATED_ROOT, DEPRECATED_SCHEMA_DIR, GROUP_MAX_DEPTH,
} from './helpers.mjs';

const current = loadCurrentSchema();
const validateCurrent = compile(current);
const index = vectorIndex();
const vectorBytes = (kind, file) => fs.readFileSync(path.join(import.meta.dirname, '..', 'test-vectors', kind, file), 'utf8');

// ---------------------------------------------------------------------------------------------
// Semantic vectors: what the schema cannot see
// ---------------------------------------------------------------------------------------------

test('every valid vector passes every semantic rule, not only the schema', () => {
  for (const entry of index.valid) {
    const faults = semanticFaults(vector('valid', entry.file));
    assert.deepEqual(faults, [], `${entry.file} breaks a semantic rule: ${JSON.stringify(faults)}`);
  }
});

test('every semantic_invalid vector passes the schema and is refused for exactly its rule', () => {
  assert.ok(index.semantic_invalid.length >= 11);
  const onDisk = fs.readdirSync(path.join(import.meta.dirname, '..', 'test-vectors', 'semantic-invalid')).sort();
  assert.deepEqual(index.semantic_invalid.map((entry) => entry.file).sort(), onDisk);
  for (const entry of index.semantic_invalid) {
    const document = vector('semantic-invalid', entry.file);
    assert.equal(document.apmap_version, currentVersion(), entry.file);
    // The schema must ACCEPT it — otherwise it belongs in invalid/, and a consumer whose semantic
    // checker is missing would still look conformant because the schema did the refusing.
    assert.ok(validateCurrent(document), `${entry.file} is refused by the schema:\n  ${describeErrors(validateCurrent.errors)}`);
    const rules = new Set(semanticFaults(document).map((fault) => fault.rule));
    assert.ok(rules.has(entry.rule), `${entry.file} was not refused for ${entry.rule}; found ${[...rules].join(', ') || 'nothing'}`);
  }
});

test('the depth bound is 32 and a chain of exactly 32 is legal', () => {
  assert.equal(GROUP_MAX_DEPTH, 32);
  const deep = vector('semantic-invalid', 'group-too-deep.apmap');
  assert.equal(deep.groups.length, 33);
  const legal = { ...deep, groups: deep.groups.slice(1) };
  assert.deepEqual(semanticFaults(legal), []);
});

test('a hostile chain of thousands of groups is refused, not a stack overflow', () => {
  const base = vector('valid', 'group-one-member.apmap');
  const groups = Array.from({ length: 20000 }, (_, i) => ({
    group_id: `grp_hostile${String(i).padStart(8, '0')}`,
    name: `G${i}`,
    members: [i < 19999 ? { kind: 'group', group_id: `grp_hostile${String(i + 1).padStart(8, '0')}` }
                        : { kind: 'brush', brush_id: 'brs_cube00000000a1' }],
  }));
  const rules = semanticFaults({ ...base, groups }).map((fault) => fault.rule);
  assert.deepEqual([...new Set(rules)], ['SEM-G-9']);
});

// ---------------------------------------------------------------------------------------------
// Authorship
// ---------------------------------------------------------------------------------------------

test('authorship is optional, closed, and sits after provenance in the document order', () => {
  assert.ok(!current.required.includes('authorship'));
  const keys = Object.keys(current.properties);
  assert.equal(keys[keys.indexOf('provenance') + 1], 'authorship');
  const def = current.$defs.authorship;
  assert.equal(def.additionalProperties, false);
  assert.equal(def.minProperties, 1);
  assert.deepEqual(def.required ?? [], []);
  assert.deepEqual(Object.keys(def.properties), ['author', 'copyright_notice', 'license']);
  assert.deepEqual(Object.values(def.properties).map((p) => p.maxLength), [256, 1024, 4096]);
});

test('authorship keeps absent, deliberately blank and written text apart', () => {
  const validate = compileDef(current, 'authorship');
  assert.equal(validate({ author: '' }), true, 'blank is a value');
  assert.equal(validate({}), false, 'nothing recorded is spelled by omitting the member');
  const blank = vector('valid', 'authorship-deliberately-blank.apmap').authorship;
  assert.deepEqual(Object.keys(blank), ['author', 'license']);
  assert.equal(blank.author, '');
  assert.ok(!('copyright_notice' in blank), 'unknown stays absent');
});

test('authorship refuses what a user cannot have meant, and nothing a user can', () => {
  const validate = compileDef(current, 'authorship');
  for (const good of ["Alice O'Neil & Bob", 'Zoë 日本 😀', '—', ' leading and trailing spaces ', 'x'.repeat(256), '😀'.repeat(256)])
    assert.equal(validate({ author: good }), true, JSON.stringify(good));
  for (const bad of ['a\nb', 'a\rb', 'a\tb', 'a\u0000b', 'a\u007fb', 'a\u0085b', 'x'.repeat(257)])
    assert.equal(validate({ author: bad }), false, JSON.stringify(bad));
  for (const field of ['copyright_notice', 'license']) {
    assert.equal(validate({ [field]: 'line one\nline two\n\n\tindented' }), true, field);
    assert.equal(validate({ [field]: 'line one\r\nline two' }), false, `${field} CRLF`);
    assert.equal(validate({ [field]: 'bell\u0007' }), false, `${field} BEL`);
  }
  assert.equal(validate({ author: 42 }), false);
  assert.equal(validate({ author: null }), false, 'null is neither unknown nor blank');
});

test('authorship text is exact bytes through parse and canonical re-serialization', () => {
  // The pinned specimens: SER-1..9 re-encoding must reproduce every byte, apostrophes, quotes,
  // em dashes, CJK, line breaks and TABs included. A reader that normalizes, trims or re-escapes
  // fails this; property-order changes cannot occur because canonical order is fixed.
  for (const file of ['authorship-full.apmap', 'authorship-deliberately-blank.apmap', 'no-1-5-features.apmap']) {
    const bytes = vectorBytes('valid', file);
    assert.equal(`${JSON.stringify(JSON.parse(bytes), null, 2)}\n`, bytes, file);
  }
  const full = vector('valid', 'authorship-full.apmap').authorship;
  assert.equal(full.author, 'Zoë O\'Neil & Bob "the Builder" Łukasz — 山田太郎');
  assert.equal(full.copyright_notice, '© 2026 Zoë O\'Neil.\nAll rights reserved — except where noted.');
  assert.ok(full.license.includes('\n\n\t'));
  // NFC and NFD spellings are different values: nothing here normalizes, so nothing may.
  assert.notEqual('Zoë'.normalize('NFD'), 'Zoë'.normalize('NFC'));
  assert.ok(compileDef(current, 'authorship')({ author: 'Zoë'.normalize('NFD') }));
});

// ---------------------------------------------------------------------------------------------
// Promotion — every deprecated corpus into the current contract
// ---------------------------------------------------------------------------------------------

const corpora = fs.readdirSync(DEPRECATED_ROOT).filter((name) => /^\d+\.\d+$/.test(name)).sort();

/** Every valid document in every deprecated corpus, with the version it declares. */
function legacyDocuments() {
  const found = [];
  for (const version of corpora) {
    const root = path.join(DEPRECATED_ROOT, version);
    const dirs = [path.join(root, 'test-vectors', 'valid'), path.join(root, 'examples')];
    for (const directory of dirs) {
      if (!fs.existsSync(directory)) continue;
      for (const name of fs.readdirSync(directory).filter((n) => n.endsWith('.apmap')).sort())
        found.push({ version, file: path.join(directory, name), document: readJson(path.join(directory, name)) });
    }
    for (const name of fs.readdirSync(root).filter((n) => n.endsWith('.apmap')))
      found.push({ version, file: path.join(root, name), document: readJson(path.join(root, name)) });
  }
  return found;
}

test('every valid legacy document is valid under its own frozen contract, then promotes into the current contract', () => {
  const documents = legacyDocuments();
  assert.ok(documents.length > 40, `only ${documents.length} legacy documents found`);
  const frozen = new Map();
  for (const { version, file, document } of documents) {
    assert.equal(document.apmap_version, version, file);
    if (!frozen.has(version)) frozen.set(version, compile(readJson(path.join(DEPRECATED_SCHEMA_DIR, `apmap-${version}.schema.json`))));
    assert.ok(frozen.get(version)(document), `${file} is not valid ${version}`);
    const promoted = promoteToCurrent(document);
    assert.ok(validateCurrent(promoted), `${file} does not promote:\n  ${describeErrors(validateCurrent.errors)}`);
  }
});

test('promotion is idempotent and changes the header alone — no authorship, nesting or patch invented', () => {
  for (const { version, file, document } of legacyDocuments()) {
    const once = promoteToCurrent(document);
    assert.deepEqual(promoteToCurrent(once), once, `${file}: promoting twice differs from once`);
    assert.deepEqual(once.authorship, document.authorship, `${file}: promotion invented or changed authorship`);
    const { apmap_version: _v, groups: _g, ...restBefore } = document;
    const { apmap_version: _w, groups: after, ...restAfter } = once;
    assert.deepEqual(restAfter, restBefore, `${file}: promotion touched something other than the header`);
    assert.deepEqual(after, document.groups ?? [], `${file}: promotion changed the groups`);
    // Only a 1.4 document can already hold a nested group or an authorship; nothing older gains one.
    if (version !== '1.4') {
      assert.ok(!('authorship' in once), `${file}: promotion invented authorship`);
      assert.ok(after.every((group) => group.members.every((member) => member.kind !== 'group')));
    }
    // And no legacy document holds a Quake III surface: 1.5 is the first that can say one.
    const text = JSON.stringify(once);
    assert.ok(!text.includes('"patch"') && !text.includes('brush_primitives'), `${file}: a Quake III surface appeared`);
  }
});

test('a legacy 1.3 one-member group is still refused — by 1.3, before any promotion', () => {
  // The relaxation is 1.4's. A file that says it is 1.3 and holds a one-member group was invalid
  // the day it was written, and a reader validates against the declared version BEFORE promoting,
  // so it never becomes a valid 1.4 document by the back door.
  const frozen13 = compile(readJson(path.join(DEPRECATED_SCHEMA_DIR, 'apmap-1.3.schema.json')));
  const lonely = readJson(path.join(DEPRECATED_ROOT, '1.3', 'test-vectors', 'invalid', 'group-one-member.apmap'));
  assert.equal(lonely.apmap_version, '1.3');
  assert.equal(frozen13(lonely), false);
  assert.ok(frozen13.errors.some((error) => error.keyword === 'minItems' && error.instancePath === '/groups/0/members'));
});

test('an unknown future version is refused by name, before anything else is read', () => {
  const future = vector('invalid', 'unknown-apmap-version.apmap');
  assert.equal(future.apmap_version, '999.999');
  assert.equal(validateCurrent(future), false);
  assert.ok(validateCurrent.errors.some((error) => error.instancePath === '/apmap_version' && error.keyword === 'enum'));
});

test('the frozen 1.3 corpus keeps its one known containment fault, recorded rather than rewritten', () => {
  // group-two-groups.apmap in the 1.3 corpus put brush a1 and a1's own face in one group. The
  // schema could not see it; 1.4's reference checker can. History stays as it was written — the
  // 1.4 descendant is the one that was corrected, and index.json says so.
  const frozen = readJson(path.join(DEPRECATED_ROOT, '1.3', 'test-vectors', 'valid', 'group-two-groups.apmap'));
  assert.deepEqual(semanticFaults(promoteToCurrent(frozen)).map((fault) => fault.rule), ['SEM-G-5']);
  assert.deepEqual(semanticFaults(vector('valid', 'group-two-groups.apmap')), []);
  assert.match(index.valid.find((entry) => entry.file === 'group-two-groups.apmap').exercises, /SEM-G-5/);
});
