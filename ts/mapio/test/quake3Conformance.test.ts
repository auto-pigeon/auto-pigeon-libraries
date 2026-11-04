/**
 * The Quake III `.map` → APMap conformance set, shared with Go (`go/mapio/quake3_conformance_test.go`).
 *
 * Two kinds of expectation, kept apart on purpose:
 *
 * - `fixtures/quake3-conformance/facts.json` — facts derived by hand from each fixture's text. They hold
 *   BOTH implementations to the fixture, so neither can drift and take the other with it.
 * - `fixtures/quake3-conformance/reference/` — this implementation's own outputs (canonical APMap text,
 *   the `.map` written back, the refusal), which the Go importer must reproduce: byte for byte except
 *   `provenance.producer` and `provenance.producer_version`, which name the implementation.
 *
 * Inputs are read as BYTES and decoded the way the editor decodes a picked file, and the digest is over
 * the bytes — the identity contract every consumer shares. `MAPIO_WRITE_QUAKE3_REFERENCE=1 npx vitest run
 * test/quake3Conformance.test.ts` rewrites the reference after a deliberate change, and the diff is the review.
 */
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import {
  MapExportError, Quake3ImportError, apmapFromQuake3Map, canonicalApmapText, mapTextFromApmap, parseApmapText,
  parseQuake3Map, sha256Hex,
} from '../src/index.ts';

const FIXTURES = new URL('./fixtures/', import.meta.url);
const CONFORMANCE = new URL('./fixtures/quake3-conformance/', import.meta.url);
const REFERENCE = new URL('./fixtures/quake3-conformance/reference/', import.meta.url);
const WRITE = process.env.MAPIO_WRITE_QUAKE3_REFERENCE === '1';

type Facts = {
  imports: Record<string, { game: string; map_dialect?: string; summary?: Record<string, unknown>; kinds?: Record<string, string[]>;
                            facts?: [string, unknown][]; export_refused?: boolean }>;
  refusals: Record<string, { code: string; first?: [string, number, number]; rejected?: unknown[] }>;
};
const facts = JSON.parse(readFileSync(new URL('facts.json', CONFORMANCE), 'utf8')) as Facts;

const importBytes = (bytes: Uint8Array, name: string) =>
  apmapFromQuake3Map(parseQuake3Map(new TextDecoder('utf-8').decode(bytes)), { sourceMap: name, sourceSha256: sha256Hex(bytes) });
const bytesOf = (name: string): Uint8Array => new Uint8Array(readFileSync(new URL(name, FIXTURES)));
const reference = (file: string, text: string): void => {
  if (WRITE) { mkdirSync(REFERENCE, { recursive: true }); writeFileSync(new URL(file, REFERENCE), text); return; }
  expect(text).toBe(readFileSync(new URL(file, REFERENCE), 'utf8'));
};
/** RFC 6901, the one path language both tests read. */
const pointer = (value: unknown, path: string): unknown =>
  path.split('/').slice(1).reduce<unknown>((at, token) => (at as Record<string, unknown> | undefined)?.[token.replace(/~1/g, '/').replace(/~0/g, '~')], value);
const refusalOf = (error: unknown) => {
  const refused = error as Quake3ImportError;
  return { code: refused.code, message: refused.message,
           parse: (refused.detail.parseErrors ?? []).map((one) => [one.code, one.range.line, one.range.column, one.entityIndex ?? -1, one.brushIndex ?? -1, one.message]),
           rejected: refused.detail.rejected ?? [] };
};

describe('Quake III conformance set', () => {
  it('covers every q3-*.map fixture, each either imported or refused', () => {
    const named = new Set([...Object.keys(facts.imports), ...Object.keys(facts.refusals)]);
    expect(readdirSync(FIXTURES).filter((name) => /^q3-.*\.map$/.test(name)).sort()).toEqual([...named].sort());
  });

  it.each(Object.entries(facts.imports))('%s imports to the facts and the reference, and round-trips to a fixed point', (name, expected) => {
    const bytes = bytesOf(name);
    const { document, summary } = importBytes(bytes, name);
    expect(document.game).toBe(expected.game);
    if (expected.map_dialect) expect(document.map_dialect).toBe(expected.map_dialect);
    if (expected.summary) expect(summary).toMatchObject(expected.summary);
    for (const [entity, kinds] of Object.entries(expected.kinds ?? {}))
      expect(document.entities[Number(entity)].content.map((item) => item.kind)).toEqual(kinds);
    for (const [path, value] of expected.facts ?? []) expect(pointer(document, path) ?? null, path).toEqual(value);

    const text = canonicalApmapText(document);
    reference(`${name}.apmap`, text);
    reference(`${name}.summary.json`, `${JSON.stringify(summary, null, 2)}\n`);

    if (expected.export_refused) {
      expect(() => mapTextFromApmap(parseApmapText(text))).toThrow(MapExportError);
      return;
    }
    // MAP -> APMap -> MAP -> APMap: the written map is a fixed point of the writer, and its import is
    // the same document but for identity, which names the bytes it came from.
    const written = mapTextFromApmap(parseApmapText(text));
    reference(`${name}.export.map`, written);
    const again = importBytes(new TextEncoder().encode(written), name);
    expect(mapTextFromApmap(again.document)).toBe(written);
    const unidentified = (value: unknown): unknown => JSON.parse(JSON.stringify(value, (key, inner) =>
      ['entity_id', 'brush_id', 'face_id', 'patch_id', 'derived_from', 'provenance'].includes(key) ? undefined : inner));
    expect(unidentified(again.document)).toEqual(unidentified(document));
    expect((again.document.provenance as { source?: { map_sha256?: string } } | undefined)?.source?.map_sha256).toBe(sha256Hex(new TextEncoder().encode(written)));
  });

  it.each(Object.entries(facts.refusals))('%s is refused whole, with the reference refusal', (name, expected) => {
    let caught: unknown;
    try { importBytes(bytesOf(name), name); } catch (error) { caught = error; }
    expect(caught).toBeInstanceOf(Quake3ImportError);
    const refusal = refusalOf(caught);
    expect(refusal.code).toBe(expected.code);
    if (expected.first) expect(refusal.parse[0].slice(0, 3)).toEqual(expected.first);
    if (expected.rejected) expect(refusal.rejected).toEqual(expected.rejected);
    reference(`${name}.refusal.json`, `${JSON.stringify(refusal, null, 2)}\n`);
  });
});
