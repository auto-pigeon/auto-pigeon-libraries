/**
 * The schema documents themselves, and the hand-built vectors for what the current version adds.
 *
 * Nothing here runs on load, on import, or against a document nobody asked about — the package
 * carries no runtime code at all, and these are tests, which is the one place validation is
 * unconditional by definition.
 */
import assert from 'node:assert/strict';
import test from 'node:test';
import {
  compile, compileDef, currentVersion, describeErrors, loadCurrentSchema, readJson,
  deprecatedSchemaPath, resolvePointer, vector, vectorIndex, DEPRECATED_SCHEMA_DIR,
} from './helpers.mjs';
import path from 'node:path';

/** The CURRENT schema, whatever the filename says today. Never named after a version here. */
const current = loadCurrentSchema();
/**
 * The frozen 1.0 schema, read by an explicit repository-local path — the only way it is reachable.
 * Used ONLY by the history test at the bottom of this file, which asserts it has not thawed.
 */
const schema10 = readJson(deprecatedSchemaPath());
const index = vectorIndex();
/** The frozen 1.1 schema. It moved into `deprecated/` when 1.2 was promoted; it was not edited. */
const schema11 = readJson(path.join(DEPRECATED_SCHEMA_DIR, 'apmap-1.1.schema.json'));
/** The frozen 1.2 schema, moved down the day 1.3 was promoted. Also not edited. */
const schema12 = readJson(path.join(DEPRECATED_SCHEMA_DIR, 'apmap-1.2.schema.json'));
/** The frozen 1.3 schema, moved down the day 1.4 was promoted. Also not edited. */
const schema13 = readJson(path.join(DEPRECATED_SCHEMA_DIR, 'apmap-1.3.schema.json'));
/** The frozen 1.4 schema, moved down the day 1.5 was promoted. Also not edited. */
const schema14 = readJson(path.join(DEPRECATED_SCHEMA_DIR, 'apmap-1.4.schema.json'));

/**
 * The relaxations in the whole history, each argued in SEMANTICS.md §13. A walk over two schemas
 * reports anything changed, removed or shortened; these paths are the only changes allowed, and each
 * is allowed only from the version that made it.
 */
const PATH_GRAMMAR_14 = '^e[0-9]+(\\.b[0-9]+(\\.f[0-9]+)?)?$';
const PATH_GRAMMAR_15 = '^e[0-9]+(\\.b[0-9]+(\\.f[0-9]+)?|\\.p[0-9]+)?$';
const RELAXED_IN_15 = new Set([
  '/$defs/derived_from_source_map/properties/source_path/pattern',
  '/$defs/derived_from_package/properties/object_path/pattern',
]);

/** Every difference between two schema documents that is not an addition, as `changed <pointer>`. */
function nonAdditiveChanges(before, after, ignore = () => false) {
  const changes = [];
  const walk = (a, b, at) => {
    if (ignore(at)) return;
    const isObject = (value) => typeof value === 'object' && value !== null && !Array.isArray(value);
    if (isObject(a) && isObject(b)) {
      for (const key of Object.keys(a)) {
        if (!(key in b)) { changes.push(`removed ${at}/${key}`); continue; }
        walk(a[key], b[key], `${at}/${key}`);
      }
      return;
    }
    if (Array.isArray(a) && Array.isArray(b)) {
      if (b.length < a.length) { changes.push(`shortened ${at}`); return; }
      a.forEach((item, position) => walk(item, b[position], `${at}/${position}`));
      return;
    }
    if (JSON.stringify(a) !== JSON.stringify(b)) changes.push(`changed ${at}`);
  };
  walk(before, after, '');
  return changes;
}

test('both schema documents compile as JSON Schema 2020-12', () => {
  assert.equal(schema10.$schema, 'https://json-schema.org/draft/2020-12/schema');
  assert.equal(current.$schema, 'https://json-schema.org/draft/2020-12/schema');
  assert.doesNotThrow(() => compile(schema10));
  assert.doesNotThrow(() => compile(current));
});

test('the current schema declares its own $id', () => {
  assert.equal(current.$id, `https://auto-pigeon.org/schemas/apmap/${currentVersion()}/apmap.schema.json`);
});

test('the frozen 1.1 schema is still pinned to 1.0 and 1.1', () => {
  // 1.1's contract as it was the day it stopped being current. It accepted 1.0 and 1.1 and had no
  // word for 1.2; if this list ever grows, the frozen copy has been edited and the record is gone.
  assert.deepEqual(schema11.properties.apmap_version.enum, ['1.0', '1.1']);
  assert.ok(!('groups' in schema11.properties), '1.1 never had groups; a retrofit would rewrite history');
  assert.ok(!schema11.required.includes('groups'));
});

test('the frozen 1.2 schema is still pinned to 1.0, 1.1 and 1.2', () => {
  // 1.2's contract as it was the day it stopped being current. It had no word for 1.3, and its
  // group object was closed over exactly three members; if either changes, the frozen copy has
  // been edited and the record of what 1.2 said is gone.
  assert.deepEqual(schema12.properties.apmap_version.enum, ['1.0', '1.1', '1.2']);
  assert.deepEqual(Object.keys(schema12.$defs.group.properties), ['group_id', 'name', 'members']);
  assert.ok(!('group_source' in schema12.$defs), '1.2 never had group provenance');
});

test('the frozen 1.2 schema refuses the member 1.3 added', () => {
  // The other half of version-matched validation, as a fact rather than a policy statement: a
  // document carrying `group.source` is NOT a 1.2 document, and the 1.2 contract says so. Were it
  // silently accepted, a mislabelled 1.3 document would pass a check it never claimed to meet.
  const sourced = vector('valid', 'group-from-prefab.apmap');
  assert.equal(compile(schema12)(sourced), false);
  // And it refuses it for the right reason: an undeclared member on a closed object.
  const group = compileDef(schema12, 'group');
  assert.equal(group(sourced.groups[0]), false);
  assert.ok((group.errors ?? []).some((error) =>
    error.keyword === 'additionalProperties' && error.params.additionalProperty === 'source'));
});

test('the frozen 1.3 schema is still pinned to two members, three kinds and no authorship', () => {
  // What 1.3 said, and what a legacy 1.3 document is still held to: the relaxation belongs to 1.4
  // alone. If any of these moves, the frozen copy has been edited to make a 1.3 file "pass".
  assert.deepEqual(schema13.properties.apmap_version.enum, ['1.0', '1.1', '1.2', '1.3']);
  assert.equal(schema13.$defs.group.properties.members.minItems, 2);
  assert.deepEqual(schema13.$defs.group_member.oneOf.map((arm) => arm.properties.kind.const), ['entity', 'brush', 'face']);
  assert.ok(!('authorship' in schema13.properties) && !('authorship' in schema13.$defs), '1.3 never had authorship');
});

test('the frozen 1.3 schema refuses everything 1.4 added or relaxed', () => {
  const frozen = compile(schema13);
  for (const file of ['group-one-member.apmap', 'group-nested-parent.apmap', 'authorship-full.apmap']) {
    const document = { ...vector('valid', file), apmap_version: '1.3' };
    assert.equal(frozen(document), false, `the frozen 1.3 contract accepted ${file}`);
  }
  // A document with none of 1.4's features, relabelled 1.3, is still a 1.3 document: the frozen
  // contract refuses what is new, not what is old.
  assert.ok(frozen({ ...vector('valid', 'group-two-brushes.apmap'), apmap_version: '1.3' }));
});

/**
 * 1.3 -> 1.4 is the first promotion that is not a pure addition. The walk below allows exactly the
 * three documented differences and nothing else, so the next "small" relaxation has to be written
 * into this list — and into SEMANTICS.md §13 — rather than slipping through.
 */
test('the frozen 1.4 schema is still pinned to groups, authorship and no Quake III surfaces', () => {
  // What 1.4 said, and what a legacy 1.4 document is still held to. If any of these moves, the
  // frozen copy has been edited to make a 1.4 file "pass".
  assert.deepEqual(schema14.properties.apmap_version.enum, ['1.0', '1.1', '1.2', '1.3', '1.4']);
  assert.equal(schema14.$defs.group.properties.members.minItems, 1);
  assert.deepEqual(schema14.$defs.group_member.oneOf.map((arm) => arm.properties.kind.const), ['entity', 'brush', 'face', 'group']);
  assert.deepEqual(schema14.$defs.content_item.oneOf.map((arm) => arm.$ref), ['#/$defs/property_item', '#/$defs/brush_item']);
  assert.deepEqual(schema14.$defs.projection.oneOf.map((arm) => arm.$ref), ['#/$defs/projection_classic', '#/$defs/projection_valve220']);
  assert.ok(!('patch_item' in schema14.$defs) && !('projection_brush_primitives' in schema14.$defs), '1.4 never had Quake III surfaces');
  assert.equal(schema14.$defs.derived_from_source_map.properties.source_path.pattern, PATH_GRAMMAR_14);
});

test('the frozen 1.4 schema refuses everything 1.5 added', () => {
  const frozen = compile(schema14);
  for (const file of ['q3-patch-minimal.apmap', 'q3-brush-primitives.apmap', 'q3-patch-in-nested-group.apmap', 'q3-patch-derived-ids.apmap']) {
    const document = { ...vector('valid', file), apmap_version: '1.4' };
    assert.equal(frozen(document), false, `the frozen 1.4 contract accepted ${file}`);
  }
  // A document with none of 1.5's features, relabelled 1.4, is still a 1.4 document.
  assert.ok(frozen({ ...vector('valid', 'no-1-5-features.apmap'), apmap_version: '1.4' }));
});

test('1.4 differs from 1.3 by additions and the one documented relaxation', () => {
  const RELAXED = new Set(['/$defs/group/properties/members/minItems']);
  const PROSE = /\/(description|title|\$id)$/;
  const changes = [];
  const walk = (before, after, at) => {
    if (PROSE.test(at) || at === '/properties/apmap_version/enum') return;
    const isObject = (value) => typeof value === 'object' && value !== null && !Array.isArray(value);
    if (isObject(before) && isObject(after)) {
      for (const key of Object.keys(before)) {
        if (!(key in after)) { changes.push(`removed ${at}/${key}`); continue; }
        walk(before[key], after[key], `${at}/${key}`);
      }
      return;
    }
    if (Array.isArray(before) && Array.isArray(after)) {
      if (after.length < before.length) { changes.push(`shortened ${at}`); return; }
      before.forEach((item, position) => walk(item, after[position], `${at}/${position}`));
      return;
    }
    if (JSON.stringify(before) !== JSON.stringify(after)) changes.push(`changed ${at}`);
  };
  walk(schema13, schema14, '');
  assert.deepEqual(changes.filter((change) => !RELAXED.has(change.replace(/^changed /, ''))), []);
  assert.equal(schema13.$defs.group.properties.members.minItems, 2);
  assert.equal(schema14.$defs.group.properties.members.minItems, 1);
  assert.deepEqual(schema14.properties.apmap_version.enum, [...schema13.properties.apmap_version.enum, '1.4']);
});

/**
 * 1.4 -> 1.5 adds three union arms (content item `patch`, projection `brush_primitives`, group member
 * `patch`) and new definitions, and makes one relaxation: the object-path grammar, in both places it
 * is spelled, admits `e<n>.p<n>` so a patch can be addressed. Nothing else may differ.
 */
test('1.5 differs from 1.4 by additions and the one documented path relaxation', () => {
  const PROSE = /\/(description|title|\$id)$/;
  const changes = nonAdditiveChanges(schema14, current,
    (at) => PROSE.test(at) || at === '/properties/apmap_version/enum');
  assert.deepEqual(changes.filter((change) => !RELAXED_IN_15.has(change.replace(/^changed /, ''))), []);
  assert.deepEqual(current.properties.apmap_version.enum, [...schema14.properties.apmap_version.enum, '1.5']);
  for (const pointer of RELAXED_IN_15) {
    const [before, after] = [schema14, current].map((schema) => pointer.split('/').slice(1).reduce((node, key) => node[key], schema));
    assert.equal(before, PATH_GRAMMAR_14, pointer);
    assert.equal(after, PATH_GRAMMAR_15, pointer);
  }
  // A relaxation, not a change of meaning: every path 1.4 accepted, 1.5 accepts, and the only new
  // spelling is an entity's patch.
  const [old14, new15] = [new RegExp(PATH_GRAMMAR_14), new RegExp(PATH_GRAMMAR_15)];
  for (const path of ['e0', 'e30', 'e30.b0', 'e30.b12.f5', 'e1.p0', 'e1.p0.f0', 'e1.b0.p0', 'p0', 'e1.x0', 'e1.b0.f0.f1'])
    assert.equal(new15.test(path), old14.test(path) || /^e[0-9]+\.p[0-9]+$/.test(path), path);
});

test('the frozen 1.0 schema is still pinned to 1.0', () => {
  // History only. If this ever changes, the frozen copy has been edited and the record of what 1.0
  // actually said is gone.
  assert.equal(schema10.properties.apmap_version.const, '1.0');
});

/**
 * HISTORY, not a support promise.
 *
 * This records that the current schema was built additively from the frozen one — which is why a
 * future migration tool can be mechanical rather than lossy. It is emphatically NOT a statement
 * that a 1.0 document is accepted at runtime: it is refused at the version gate, before validation.
 * The test reads the deprecated schema by an explicit path for exactly that reason.
 */
test('the current schema differs from the frozen one only by additions and the documented relaxations', () => {
  // 1.4's minItems relaxation is on a member 1.0 did not have; 1.5's path relaxation is on members
  // 1.0 did have, and is the only change this walk admits — see the 1.5 walk above for its proof.
  const ALLOWED = new Set(['/$id', '/title', '/description', '/properties/apmap_version', ...RELAXED_IN_15]);
  const changes = [];
  const walk = (before, after, path) => {
    if (ALLOWED.has(path)) return;
    const isObject = (value) => typeof value === 'object' && value !== null && !Array.isArray(value);
    if (isObject(before) && isObject(after)) {
      for (const key of Object.keys(before)) {
        if (!(key in after)) { changes.push(`removed ${path}/${key}`); continue; }
        walk(before[key], after[key], `${path}/${key}`);
      }
      return;
    }
    if (Array.isArray(before) && Array.isArray(after)) {
      // An added `oneOf` branch is an addition; a changed or dropped one is not.
      if (after.length < before.length) { changes.push(`shortened ${path}`); return; }
      before.forEach((item, position) => walk(item, after[position], `${path}/${position}`));
      return;
    }
    if (JSON.stringify(before) !== JSON.stringify(after)) changes.push(`changed ${path}`);
  };
  walk(schema10, current, '');
  assert.deepEqual(changes, [], `1.1 changed something 1.0 already defined:\n  ${changes.join('\n  ')}`);
});

test('derived_from defines exactly the five known kinds', () => {
  const kinds = current.$defs.derived_from.oneOf.map((branch) => {
    const def = branch.$ref.replace('#/$defs/', '');
    return current.$defs[def].properties.kind.const;
  });
  assert.deepEqual(kinds.sort(), ['authored', 'operation', 'package', 'source_map', 'synthetic']);
});

test('the operation kind requires only kind and operation_id', () => {
  assert.deepEqual(current.$defs.derived_from_operation.required, ['kind', 'operation_id']);
  assert.equal(current.$defs.derived_from_operation.additionalProperties, false);
});

test('intent and broken carry their intent in their descriptions', () => {
  // Both are load-bearing prose: an implementer reading only the schema must inherit them.
  assert.match(current.$defs.derived_from_operation.properties.intent.description, /never authorization/i);
  assert.match(current.$defs.brush_item.properties.broken.description, /warns and exports anyway/i);
  assert.equal(current.$defs.brush_item.properties.broken.type, 'boolean');
  assert.ok(!(current.$defs.brush_item.required ?? []).includes('broken'), 'broken must stay optional');
});

const validateCurrent = compile(current);

test('every valid vector is accepted by the current schema', (t) => {
  assert.ok(index.valid.length > 0);
  for (const entry of index.valid) {
    const document = vector('valid', entry.file);
    assert.ok(validateCurrent(document), `${entry.file} was rejected:\n  ${describeErrors(validateCurrent.errors)}`);
    t.diagnostic(`accepted ${entry.file}`);
  }
});

test('the frozen 1.0 schema still refuses a current document', () => {
  // If this ever passes, the frozen copy has been edited and old documents lost their fixed reference.
  const validate10 = compile(schema10);
  assert.equal(validate10(vector('valid', 'broken-brush.apmap')), false);
});

test('every current vector declares the current version', () => {
  // The set is current-only now: a document declaring a deprecated version is not a vector here,
  // it is a rejection fixture under deprecated/.
  for (const entry of index.valid)
    assert.equal(vector('valid', entry.file).apmap_version, currentVersion(), entry.file);
});

test('every invalid vector is rejected by the current schema', () => {
  assert.ok(index.invalid.length > 0);
  for (const entry of index.invalid) {
    const document = vector('invalid', entry.file);
    assert.equal(validateCurrent(document), false, `${entry.file} (${entry.rule}) was accepted`);
  }
});

test('every invalid vector is rejected for the recorded reason', () => {
  for (const entry of index.invalid) {
    const document = vector('invalid', entry.file);
    const expectation = entry.expect_fragment ?? entry.expect_document;
    const [validate, subject] = entry.expect_fragment
      ? [compileDef(current, entry.expect_fragment.def), resolvePointer(document, entry.expect_fragment.pointer)]
      : [validateCurrent, document];
    assert.equal(validate(subject), false, `${entry.file}: the fragment named in index.json is valid`);
    const matched = (validate.errors ?? []).filter((error) =>
      error.keyword === expectation.keyword
      && error.instancePath === expectation.instancePath
      && (expectation.missingProperty === undefined || error.params.missingProperty === expectation.missingProperty)
      && (expectation.additionalProperty === undefined || error.params.additionalProperty === expectation.additionalProperty));
    assert.ok(matched.length > 0,
      `${entry.file} (${entry.rule}) failed, but not for ${JSON.stringify(expectation)}. Reported:\n  ${describeErrors(validate.errors)}`);
  }
});

// ---------------------------------------------------------------------------------------------
// What 1.2 added: groups
// ---------------------------------------------------------------------------------------------

test('a current writer must emit groups, empty or not', () => {
  // Required, deliberately. An OPTIONAL groups member would make "this map has no groups" and
  // "this producer has never heard of groups" the same bytes, and only one of those is safe to
  // round-trip through a tool that drops what it does not understand.
  assert.ok(current.required.includes('groups'));
  assert.equal(current.properties.groups.type, 'array');
  assert.deepEqual(current.properties.groups.items, { $ref: '#/$defs/group' });
});

test('groups sits between entities and relationships in the document order', () => {
  // Canonical serialization order is a contract of its own: a serializer that invents another
  // order produces documents that differ byte-wise from the same document written elsewhere.
  const keys = Object.keys(current.properties);
  assert.equal(keys[keys.indexOf('entities') + 1], 'groups');
  assert.ok(keys.indexOf('groups') < keys.indexOf('relationships'));
});

test('a group is group_id, name, members and nothing but an optional source', () => {
  const group = current.$defs.group;
  assert.equal(group.additionalProperties, false);
  assert.deepEqual(group.required, ['group_id', 'name', 'members']);
  assert.deepEqual(Object.keys(group.properties), ['group_id', 'name', 'members', 'source']);
});

test('a group name is a bounded, non-blank label — never identity', () => {
  const name = current.$defs.group.properties.name;
  assert.equal(name.minLength, 1);
  assert.equal(name.maxLength, 128);
  // `\S` is how JSON Schema says "not blank": minLength alone accepts three spaces.
  assert.equal(name.pattern, '\\S');
  const validate = compileDef(current, 'group');
  const members = [{ kind: 'brush', brush_id: 'brs_cube00000000a1' }, { kind: 'brush', brush_id: 'brs_cube00000000b2' }];
  assert.equal(validate({ group_id: 'grp_blankname00001', name: '   ', members }), false);
  assert.equal(validate({ group_id: 'grp_realname000001', name: 'Entrance Columns', members }), true);
  // Identity never comes from the name: two groups may carry the same label.
  assert.equal(current.$defs.group_id.pattern, '^grp_[0-9A-Za-z]{8,64}$');
});

test('a persisted group holds at least ONE distinct member — 1.4 relaxed two', () => {
  const validate = compileDef(current, 'group');
  const one = { kind: 'brush', brush_id: 'brs_cube00000000a1' };
  const two = { kind: 'brush', brush_id: 'brs_cube00000000b2' };
  assert.equal(validate({ group_id: 'grp_one0000000001', name: 'One', members: [one] }), true, 'one member');
  assert.equal(validate({ group_id: 'grp_none000000001', name: 'None', members: [] }), false, 'empty');
  assert.equal(validate({ group_id: 'grp_dup0000000001', name: 'Dup', members: [one, one] }), false, 'duplicate');
  assert.equal(validate({ group_id: 'grp_two0000000001', name: 'Two', members: [one, two] }), true);
});

test('a member is a discriminated union, one id field per kind', () => {
  const validate = compileDef(current, 'group_member');
  assert.equal(validate({ kind: 'entity', entity_id: 'ent_light000000001' }), true);
  assert.equal(validate({ kind: 'brush', brush_id: 'brs_cube00000000a1' }), true);
  assert.equal(validate({ kind: 'face', face_id: 'fac_00cube0000' }), true);
  // The point of the union: a kind cannot carry another kind's id, and there is no untyped
  // object_id to smuggle one through.
  assert.equal(validate({ kind: 'entity', entity_id: 'brs_cube00000000a1' }), false, 'brush id under entity');
  assert.equal(validate({ kind: 'brush', object_id: 'brs_cube00000000a1' }), false, 'untyped object_id');
  assert.equal(validate({ kind: 'brush', brush_id: 'brs_cube00000000a1', label: 'left' }), false, 'extra member');
  // 1.4's fourth arm: a group may name another group, by group_id and nothing else.
  assert.equal(validate({ kind: 'group', group_id: 'grp_nested00000001' }), true, 'group nesting');
  assert.equal(validate({ kind: 'group', group_id: 'brs_cube00000000a1' }), false, 'brush id under group');
  assert.equal(validate({ kind: 'group', group_id: 'grp_nested00000001', name: 'x' }), false, 'a member is a reference only');
});

// ---------------------------------------------------------------------------------------------
// What 1.3 added: where a group came from
// ---------------------------------------------------------------------------------------------

test('a group source is optional, so promoting a 1.2 document invents nothing', () => {
  // The whole reason the member is optional. A 1.2 group has no origin recorded, and the only
  // truthful promotion of "no origin recorded" is to say nothing — not to guess one, and not to
  // write a null that a reader would have to learn to disbelieve.
  assert.ok(!current.$defs.group.required.includes('source'));
  const validate = compileDef(current, 'group');
  const members = [{ kind: 'brush', brush_id: 'brs_cube00000000a1' }, { kind: 'brush', brush_id: 'brs_cube00000000b2' }];
  assert.equal(validate({ group_id: 'grp_nosource000001', name: 'Hand made', members }), true);
});

test('a group source is a closed prefab reference and nothing else', () => {
  const source = current.$defs.group_source;
  assert.equal(source.additionalProperties, false);
  assert.deepEqual(source.required, ['kind', 'prefab_id']);
  assert.deepEqual(Object.keys(source.properties), ['kind', 'prefab_id']);
  assert.equal(source.properties.kind.const, 'prefab');
  assert.equal(source.properties.prefab_id.type, 'string');
  assert.equal(source.properties.prefab_id.minLength, 1);
});

test('a group source records identity, never the prefab library asset', () => {
  // The policy, as a test rather than a paragraph: what may go in is one id. A screenshot URL, the
  // prefab's title, its owner, its revision — all of them are the library's, all of them go stale,
  // and a copy in the map would be an unauthenticated second source of truth for somebody else's
  // asset. `additionalProperties: false` is what enforces it; this names the members it enforces
  // it against, so a later "just one more field" has to argue with a test.
  const validate = compileDef(current, 'group_source');
  const good = { kind: 'prefab', prefab_id: 'user/qk3m2p9x7v1a0zt/9f2c41ab7d0e6538' };
  assert.equal(validate(good), true);
  for (const extra of ['preview_image_url', 'screenshot', 'title', 'name', 'owner',
                       'package_revision', 'candidate_id', 'job_id'])
    assert.equal(validate({ ...good, [extra]: 'x' }), false, `${extra} was accepted into group.source`);
});

test('a group source names a prefab that can actually be named', () => {
  const validate = compileDef(current, 'group_source');
  // Empty provenance is worse than none: it claims an origin no reader can ever resolve.
  assert.equal(validate({ kind: 'prefab', prefab_id: '' }), false, 'empty prefab_id');
  assert.equal(validate({ kind: 'prefab' }), false, 'no prefab_id at all');
  // A new kind of origin is a schema change, never a producer's invention.
  assert.equal(validate({ kind: 'annotation', prefab_id: 'x' }), false, 'unknown kind');
  assert.equal(validate({ prefab_id: 'x' }), false, 'no kind');
  // The identity is the LIBRARY's, so the format imposes no shape on it beyond bounded and
  // non-empty. AUB's user prefabs are `user/<owner>/<digest>` — slashes and all — and an APMap
  // id pattern here would refuse a legal reference.
  assert.equal(validate({ kind: 'prefab', prefab_id: 'user/qk3m2p9x7v1a0zt/9f2c41ab7d0e6538' }), true);
  assert.equal(validate({ kind: 'prefab', prefab_id: 'x'.repeat(128) }), true);
  assert.equal(validate({ kind: 'prefab', prefab_id: 'x'.repeat(129) }), false, 'unbounded prefab_id');
});

test('source is not identity: two groups may share one, and it never becomes a member', () => {
  // Provenance is historical, so two placements of the same prefab produce two groups that both
  // remember it. Nothing dedupes on it, nothing resolves it against this document, and it is not
  // an object id — which is why it carries no APMap prefix and is not in `group_member` at all.
  const validate = compile(current);
  const document = vector('valid', 'group-from-prefab.apmap');
  const twice = { ...document, groups: [document.groups[0],
    { ...document.groups[0], group_id: 'grp_secondplacing1' }] };
  assert.ok(validate(twice), `two groups from one prefab were rejected:\n  ${describeErrors(validate.errors)}`);
  const member = compileDef(current, 'group_member');
  assert.equal(member({ kind: 'prefab', prefab_id: 'user/x/y' }), false, 'source is not a member kind');
});
