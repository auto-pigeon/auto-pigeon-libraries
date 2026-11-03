/**
 * The APMap contract as `@auto-pigeon/apmap-schema` states it: the current version written, every legacy
 * version read and promoted at the decode door, explicit (never automatic) validation that tells valid,
 * schema-invalid and semantic-invalid documents apart, Quake II export admission, identity and provenance.
 */
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import {
  ApmapDecodeError, MapExportError, analyseApmapIdentity, apmapReadableVersions, canonicalApmapText, currentApmapVersion,
  deriveApmapId, groupGraphFaults, inspectApmapText, isApmapReadable, mapTextFromApmap, parseApmapText, quake3SurfaceFaults,
  sourceMapDerivedFrom,
} from '../src/index.ts';
import { CURRENT_APMAP_VERSION, CURRENT_SCHEMA, LEGACY_VERSIONS, PACKAGE_DIR, READABLE_SCHEMAS, vector } from './support/contract.ts';

type Entry = { file: string; rule?: string };
const INDEX = JSON.parse(vector('test-vectors/index.json')) as Record<'valid' | 'invalid' | 'semantic_invalid' | 'quake2_admission', Entry[]>;
const inspect = (text: string) => inspectApmapText(text, { schema: CURRENT_SCHEMA, schemas: READABLE_SCHEMAS });
const errors = (text: string, stage: string) => inspect(text).issues.filter((issue) => (issue.severity === 'error' || issue.severity === 'fatal') && issue.stage === stage);

describe('the version set is the contract package\'s filenames', () => {
  it('writes the current version and reads every legacy one', () => {
    expect(currentApmapVersion()).toBe(CURRENT_APMAP_VERSION);
    expect([...apmapReadableVersions()].sort()).toEqual(Object.keys(READABLE_SCHEMAS).sort());
    for (const version of LEGACY_VERSIONS) expect(isApmapReadable(version)).toBe(true);
  });

  it('promotes every legacy example to the current version at the decode door, keeping its content', () => {
    const legacy = readdirSync(join(PACKAGE_DIR, 'deprecated')).flatMap((version) =>
      readdirSync(join(PACKAGE_DIR, 'deprecated', version)).filter((name) => name.endsWith('.apmap')).map((name) => `deprecated/${version}/${name}`));
    expect(legacy.length).toBe(LEGACY_VERSIONS.length);
    for (const path of legacy) {
      const source = JSON.parse(vector(path));
      const document = parseApmapText(vector(path));
      expect(LEGACY_VERSIONS).toContain(source.apmap_version);
      expect(document.apmap_version).toBe(CURRENT_APMAP_VERSION);
      expect(document.entities.length).toBe(source.entities.length);
      expect(errors(vector(path), 'schema')).toEqual([]);
    }
  });

  it('refuses an unknown version with a typed error that names it', () => {
    expect(() => parseApmapText(JSON.stringify({ apmap_version: '999.999', entities: [] }))).toThrow(ApmapDecodeError);
    expect(() => parseApmapText('{not json')).toThrow(ApmapDecodeError);
  });
});

describe('conformance vectors, read and inspected on demand', () => {
  it.each(INDEX.valid.map((entry) => entry.file))('valid %s: decodes, re-serializes stably, passes schema and semantics', (file) => {
    const text = vector(`test-vectors/valid/${file}`);
    const document = parseApmapText(text);
    const canonical = canonicalApmapText(document);
    expect(canonicalApmapText(parseApmapText(canonical))).toBe(canonical);
    expect(errors(text, 'schema')).toEqual([]);
    expect(errors(text, 'semantic')).toEqual([]);
    expect(groupGraphFaults(document)).toEqual([]);
    expect(quake3SurfaceFaults(document)).toEqual([]);
  });

  it.each(INDEX.invalid.map((entry) => entry.file))('invalid %s: the schema stage refuses it', (file) => {
    const text = vector(`test-vectors/invalid/${file}`);
    expect(errors(text, 'schema').length).toBeGreaterThan(0);
  });

  it.each(INDEX.semantic_invalid.map((entry) => [entry.file, entry.rule]))('semantic-invalid %s (%s): passes the schema, refused semantically', (file) => {
    const text = vector(`test-vectors/semantic-invalid/${file}`);
    expect(errors(text, 'schema')).toEqual([]);
    expect(errors(text, 'semantic').length).toBeGreaterThan(0);
  });

  it.each(INDEX.quake2_admission.map((entry) => [entry.file, entry.rule]))('Quake II admission %s (%s): .map export refused whole', (file) => {
    const document = parseApmapText(vector(`test-vectors/quake2-admission/${file}`));
    expect(() => mapTextFromApmap(document)).toThrow(MapExportError);
  });
});

describe('identity and provenance', () => {
  it('derives the same id from the same source address, and a different one from another', () => {
    const derived = sourceMapDerivedFrom('e0.b0.f0', 'room.map', 'a'.repeat(64));
    expect(deriveApmapId('face', derived)).toBe(deriveApmapId('face', sourceMapDerivedFrom('e0.b0.f0', 'room.map', 'a'.repeat(64))));
    expect(deriveApmapId('face', derived)).not.toBe(deriveApmapId('face', sourceMapDerivedFrom('e0.b0.f1', 'room.map', 'a'.repeat(64))));
  });

  it('finds nothing to repair in a conforming document', () => {
    for (const file of ['q3-patch-derived-ids.apmap', 'operation-derived-brush.apmap', 'group-from-prefab.apmap'])
      expect(analyseApmapIdentity(parseApmapText(vector(`test-vectors/valid/${file}`))).status).toBe('clean');
  });
});

it('validation is a call, never a side effect: decoding an invalid document does not inspect it', () => {
  const path = join(PACKAGE_DIR, 'test-vectors', 'semantic-invalid', 'group-self-member.apmap');
  expect(() => parseApmapText(readFileSync(path, 'utf8'))).not.toThrow();
});
