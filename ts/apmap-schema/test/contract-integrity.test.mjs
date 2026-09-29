/**
 * The shape of the contract itself — §3.2.
 *
 * These are the assertions every service's startup loader depends on being true of this package.
 * If any of them fails, four services fail to start, and they should: the alternative is a stack
 * that boots and then disagrees with itself about which formats it reads and which one it writes.
 *
 *     READ       current + every schema under schema/deprecated/
 *     WRITE      the one schema directly under schema/
 *     VALIDATE   with the schema matching the document's declared version
 */
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import {
  CURRENT_SCHEMA_PATTERN, DEPRECATED_ROOT, DEPRECATED_SCHEMA_DIR, PACKAGE_ROOT, SCHEMA_DIR, compile,
  currentSchemaFiles, currentSchemaPath, currentVersion, deprecated10, deprecatedSchemaPath,
  legacySchemaFiles, loadContractBundle, loadCurrentSchema, readJson, readableVersions,
} from './helpers.mjs';

test('exactly one current schema sits directly under schema/', () => {
  const found = currentSchemaFiles();
  assert.deepEqual(found, ['apmap-1.4.schema.json'],
    `schema/ must hold exactly one apmap-*.schema.json; found: ${found.join(', ') || '(none)'}`);
});

test('the current schema is valid JSON and compiles', () => {
  const schema = loadCurrentSchema();
  assert.equal(schema.$schema, 'https://json-schema.org/draft/2020-12/schema');
  assert.doesNotThrow(() => compile(schema));
});

test('the current version is derived from the filename, not declared anywhere', () => {
  assert.equal(currentVersion(), '1.4');
  // The derivation, spelled out: nothing reads a version constant to get this.
  const derived = CURRENT_SCHEMA_PATTERN.exec(path.basename(currentSchemaPath()))[1];
  assert.equal(derived, currentVersion());
});

test('the schema agrees with its own filename', () => {
  // §2.1's "verify the schema's apmap_version contract is compatible with that version". A file
  // named apmap-2.0.schema.json whose contract cannot express 2.0 is a packaging error, and every
  // service treats it as a fatal startup error.
  const contract = loadCurrentSchema().properties.apmap_version;
  const accepted = contract.const ? [contract.const] : contract.enum;
  assert.ok(accepted.includes(currentVersion()),
    `the schema's apmap_version contract ${JSON.stringify(accepted)} cannot express ${currentVersion()}`);
});

/**
 * The current schema's enum still contains "1.0", and that is deliberate and harmless.
 *
 * It means the current schema would structurally accept a 1.0-shaped document. That is not how a
 * 1.0 document gets read: a reader picks the schema MATCHING the declared version, so a 1.0
 * document is validated against the frozen 1.0 contract and never against this one. Asserted
 * rather than left implicit so that nobody later "fixes" the enum believing it is the mechanism by
 * which either the READ or the WRITE rule holds. It is neither. Version-matched validation is the
 * first; one direct schema file is the second.
 */
test('the current schema structurally accepting a 1.0 shape is not how 1.0 is read', () => {
  const contract = loadCurrentSchema().properties.apmap_version;
  assert.ok(contract.enum.includes('1.0'));
  assert.ok(contract.enum.includes(currentVersion()));
});

test('the deprecated schemas are readable legacy, and can never be the writer contract', () => {
  assert.ok(fs.existsSync(deprecatedSchemaPath()), 'the frozen 1.0 schema is a runtime read contract');
  // It lives one level down, so the CURRENT scan — which reads direct children only — cannot see
  // it. That is the mechanism: depth is what separates "readable" from "writable". A reader
  // enumerating deprecated/ finds it deliberately; a writer never can.
  assert.equal(path.dirname(deprecatedSchemaPath()), DEPRECATED_SCHEMA_DIR);
  assert.ok(!currentSchemaFiles().includes('apmap-1.0.schema.json'));
  // 1.1 joined 1.0 down here when 1.2 was promoted. Its file was MOVED, not rewritten: a frozen
  // contract that gets edited stops being the record of what that version said.
  assert.ok(!currentSchemaFiles().includes('apmap-1.1.schema.json'));
  // 1.2 followed the same way when 1.3 was promoted, and by the same mechanism.
  assert.ok(!currentSchemaFiles().includes('apmap-1.2.schema.json'));
  // And 1.3 when 1.4 was promoted — the first promotion whose successor RELAXED a rule (a group
  // may hold one member), which is exactly why the frozen 1.3 file must keep saying two.
  assert.ok(!currentSchemaFiles().includes('apmap-1.3.schema.json'));
  assert.deepEqual(legacySchemaFiles(),
    ['apmap-1.0.schema.json', 'apmap-1.1.schema.json', 'apmap-1.2.schema.json', 'apmap-1.3.schema.json']);
});

test('the readable versions are derived from the layout, not from a maintained list', () => {
  assert.deepEqual(readableVersions(), ['1.0', '1.1', '1.2', '1.3', '1.4']);
  const bundle = loadContractBundle();
  assert.equal(bundle.current.version, currentVersion());
  assert.equal(bundle.current.version, '1.4');
  // The current version is always readable — a build that could write a document it could not
  // read back would be a contract nobody could use.
  assert.ok(bundle.readable.has(bundle.current.version));
  // Every readable version's version came from its own filename.
  for (const [version, entry] of bundle.readable)
    assert.equal(CURRENT_SCHEMA_PATTERN.exec(path.basename(entry.path))[1], version);
});

test('every advertised readable contract parses and compiles', () => {
  // A bundle that advertises 1.0 and cannot compile its 1.0 schema has lied about its capability,
  // and the lie would surface at a user's first legacy import rather than at startup.
  for (const [version, entry] of loadContractBundle().readable)
    assert.doesNotThrow(() => compile(entry.schema), `APMap ${version} does not compile`);
});

test('a deprecated schema is never a writer choice', () => {
  const manifest = readJson(path.join(PACKAGE_ROOT, 'package.json'));
  const targets = Object.values(manifest.exports);
  assert.ok(!targets.some((target) => target.includes('deprecated')),
    'an export points into a deprecated path');
  assert.ok(!Object.keys(manifest.exports).some((subpath) => /\d+\.\d+/.test(subpath)),
    'a per-version export subpath exists; a consumer that can choose a version can choose a wrong one');
  assert.equal(manifest.exports['.'], `./schema/${path.basename(currentSchemaPath())}`);
});

test('the package ships the whole contract bundle, current and legacy', () => {
  const manifest = readJson(path.join(PACKAGE_ROOT, 'package.json'));
  // `schema` carries schema/deprecated/ with it, which is the point: a runtime reader consumes the
  // contract DIRECTORY, so a published tarball missing the legacy contract would be a build that
  // cannot open the maps its own users already have.
  assert.deepEqual(manifest.files.filter((entry) => entry.startsWith('schema')), ['schema']);
  // The historical CORPUS is a different thing and stays unpublished — it is evidence, not input.
  assert.ok(!manifest.files.includes('deprecated'));
});

test('the deprecated 1.0 corpus is present, frozen, and documented', () => {
  // Kept as evidence. `AGENTS.md` §4D's rule applies: this is checked-in fixture material, so a
  // missing file is a repository packaging error and fails rather than skips.
  assert.ok(fs.existsSync(deprecated10('README.md')), 'the quarantine must say what it is');
  for (const kind of ['valid', 'invalid']) {
    const index = readJson(deprecated10('test-vectors', kind, 'index.json'));
    assert.equal(index.apmap_version, '1.0');
    assert.ok(index.vectors.length > 0);
    const onDisk = fs.readdirSync(deprecated10('test-vectors', kind))
      .filter((name) => name.endsWith('.apmap')).sort();
    assert.deepEqual(index.vectors.map((entry) => entry.file).sort(), onDisk);
  }
  assert.equal(readJson(deprecated10('one-zero-document.apmap')).apmap_version, '1.0',
    'the canonical rejection fixture must still declare the deprecated version');
});

/**
 * Every deprecated corpus except 1.0's, which predates this layout and has its own test above.
 *
 * Derived from `schema/deprecated/`, not listed: promoting a version adds a corpus directory and
 * this test picks it up without being edited. That is the same rule the loaders follow, applied to
 * the evidence rather than to the contract.
 */
const quarantined = legacySchemaFiles()
  .map((name) => CURRENT_SCHEMA_PATTERN.exec(name)[1])
  .filter((version) => version !== '1.0');

test('every deprecated corpus is present, frozen, and documented', () => {
  // The same quarantine 1.0 got, applied to each version the day its successor was promoted. Same
  // rule: checked-in fixture material, so a missing file fails rather than skips.
  assert.ok(quarantined.length > 0);
  for (const version of quarantined) {
    const at = (...segments) => path.join(DEPRECATED_ROOT, version, ...segments);
    assert.ok(fs.existsSync(at('README.md')), `the ${version} quarantine must say what it is`);
    const index = readJson(at('test-vectors', 'index.json'));
    assert.equal(index.apmap_version, version);
    for (const kind of ['valid', 'invalid']) {
      const onDisk = fs.readdirSync(at('test-vectors', kind)).filter((n) => n.endsWith('.apmap')).sort();
      assert.deepEqual(index[kind].map((entry) => entry.file).sort(), onDisk);
    }
    // Only the VALID set declares this version. One invalid vector in each corpus declares a
    // version that corpus had never heard of, which is exactly what it is testing — 1.1 refused
    // "1.2" then, and 1.2 refused "1.3". BOTH of those sentinels were later promoted into real
    // versions, which is why the CURRENT set stopped using the next minor for the job.
    for (const entry of index.valid)
      assert.equal(readJson(at('test-vectors', 'valid', entry.file)).apmap_version, version,
        `${entry.file} is not a ${version} document`);

    // The canonical rejection fixture: the one `.apmap` sitting directly in the corpus directory.
    // Found rather than named, so the next promotion adds a file and edits no test.
    const fixtures = fs.readdirSync(at()).filter((name) => name.endsWith('.apmap'));
    assert.deepEqual(fixtures.length, 1, `${version} must keep exactly one rejection fixture`);
    assert.equal(readJson(at(fixtures[0])).apmap_version, version,
      'the canonical rejection fixture must still declare the deprecated version');

    // And it is a VALID document of its own version, so a consumer proving its gate handles that
    // version knows the outcome was about the version and not about a second fault in the fixture.
    const frozen = compile(readJson(path.join(DEPRECATED_SCHEMA_DIR, `apmap-${version}.schema.json`)));
    assert.ok(frozen(readJson(at(fixtures[0]))));
    for (const entry of index.valid)
      assert.ok(frozen(readJson(at('test-vectors', 'valid', entry.file))),
        `${entry.file} is no longer accepted by the frozen ${version} contract`);
  }
});
