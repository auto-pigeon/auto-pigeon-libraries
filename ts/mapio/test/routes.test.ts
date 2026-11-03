/**
 * Every whole-document route this package owns, on authored fixtures: read → APMap → canonical text →
 * decode → `.map` → read again, with the exact texture mapping, face tails, property order, patches and
 * brush-primitives matrices compared — never a rendering view, never a normalized comparison.
 */
import { describe, expect, it } from 'vitest';
import {
  ApmapDecodeError, ApmapEncodeError, MapExportError, Quake3ImportError, apmapFromParsedMap, apmapFromQuake3Map,
  apmapFromQuake3MapAsync, canonicalApmapText, isApmapBrush, isApmapPatch, mapTextFromApmap, parseApmapText,
  parseQuake3Map, parseQuake3MapAsync, parseQuakeMap, sha256Hex,
  type ApmapDocument, type ApmapEntity,
} from '../src/index.ts';
import { CURRENT_APMAP_VERSION, fixture } from './support/contract.ts';

const encodeQuake = (name: string, game?: string): ApmapDocument => {
  const text = fixture(name);
  const parsed = parseQuakeMap(text);
  expect(parsed.errors).toEqual([]);
  return apmapFromParsedMap(parsed, { sourceMap: name, sourceText: text, ...(game ? { game } : {}) });
};
const faces = (document: ApmapDocument) => document.entities.flatMap((entity) => entity.content.filter(isApmapBrush).flatMap((brush) => brush.faces));
const properties = (entity: ApmapEntity) => entity.content.filter((item) => item.kind === 'property').map((item) => [(item as { key: string }).key, (item as { value: string }).value]);
/** Read the exported text back with the same route and compare everything but provenance (which names the source). */
const reread = (document: ApmapDocument, name: string, game?: string) => {
  const text = mapTextFromApmap(document);
  return apmapFromParsedMap(parseQuakeMap(text), { sourceMap: name, sourceText: fixture(name), ...(game ? { game } : {}) });
};

describe('Quake 1 .map', () => {
  it('Classic: encodes to the current contract, round-trips its canonical text and its .map text exactly', () => {
    const document = encodeQuake('q1-classic-room.map');
    expect(document.apmap_version).toBe(CURRENT_APMAP_VERSION);
    expect([document.game, document.map_dialect]).toEqual(['quake1', 'quake_standard']);
    const text = canonicalApmapText(document);
    expect(canonicalApmapText(parseApmapText(text))).toBe(text);
    const again = reread(parseApmapText(text), 'q1-classic-room.map');
    expect(canonicalApmapText(again)).toBe(text);
  });

  it('keeps repeated keys and their order — the entity reads last-wins, the document keeps both', () => {
    const door = encodeQuake('q1-classic-room.map').entities.find((entity) => properties(entity).some(([key, value]) => key === 'classname' && value === 'func_door'))!;
    expect(properties(door)).toEqual([['classname', 'func_door'], ['target', 't1'], ['angle', '-1'], ['target', 't2']]);
    expect(mapTextFromApmap({ ...encodeQuake('q1-classic-room.map'), entities: [door] })).toMatch(/"target" "t1"[\s\S]*"target" "t2"/);
  });

  it('Valve 220: keeps the axes, shifts, rotation and scales of every face across save and reload', () => {
    const document = encodeQuake('q1-valve220-room.map');
    expect(document.map_dialect).toBe('valve_220');
    const before = faces(document).map((face) => face.projection);
    expect(before.every((projection) => projection.mode === 'valve220')).toBe(true);
    const reloaded = parseApmapText(canonicalApmapText(document));
    expect(faces(reloaded).map((face) => face.projection)).toEqual(before);
    expect(faces(reread(reloaded, 'q1-valve220-room.map')).map((face) => face.projection)).toEqual(before);
    expect(mapTextFromApmap(reloaded)).toContain('[ 0.707107 0.707107 0 0 ] [ 0.707107 -0.707107 0 0 ] 45 0.5 0.25');
  });

  it('refuses an unreadable brush whole, with a typed error — never a partial document', () => {
    const parsed = parseQuakeMap(fixture('q1-malformed.map'));
    expect(parsed.errors.map((error) => error.code)).toContain('INVALID_FACE');
    expect(() => apmapFromParsedMap(parsed, { sourceMap: 'q1-malformed.map' })).toThrow(ApmapEncodeError);
  });
});

describe('Quake II .map', () => {
  const document = encodeQuake('q2-tails.map', 'quake2');

  it('keeps an explicit zero tail and an absent tail apart', () => {
    const tails = faces(document).slice(0, 6).map((face) => face.tail);
    expect(tails).toEqual([[0, 0, 0], [0, 0, 0], [1, 16, 300], [1, 16, 300], undefined, undefined]);
    const exported = mapTextFromApmap(parseApmapText(canonicalApmapText(document)));
    expect(exported).toContain('e1u1/floor1_3 0 0 0 1 1 0 0 0\n');
    expect(exported).toContain('( 64 64 0 ) ( -64 64 0 ) ( -64 64 -16 ) e1u1/floor1_3 0 0 0 1 1\n');
  });

  it('round-trips through .map with every tail and key in order', () => {
    expect(canonicalApmapText(reread(document, 'q2-tails.map', 'quake2'))).toBe(canonicalApmapText(document));
    const wall = document.entities[2];
    expect(properties(wall)).toEqual([['classname', 'func_wall'], ['spawnflags', '1'], ['targetname', 'w'], ['spawnflags', '3']]);
  });

  it('refuses a Quake II tail no compiler reads (ADM-Q2), before a byte is written', () => {
    const bad = structuredClone(document);
    faces(bad)[0].tail = [0, 0, 0.5];
    expect(() => mapTextFromApmap(bad)).toThrow(MapExportError);
    try { mapTextFromApmap(bad); } catch (error) { expect((error as MapExportError).code).toBe('map_export_quake2_tail_not_integer'); }
  });
});

describe('Quake III .map', () => {
  const importOf = (name: string) => {
    const text = fixture(name);
    return apmapFromQuake3Map(parseQuake3Map(text), { sourceMap: name, sourceSha256: sha256Hex(text) });
  };

  it('carries every patch control point and every brushDef matrix verbatim, through canonical text and .map', () => {
    const { document, summary } = importOf('q3-brushdef-patches.map');
    expect(summary).toMatchObject({ brushPrimitiveBrushes: 2, patches: 3, redundantPlanes: 0 });
    const patches = document.entities.flatMap((entity) => entity.content.filter(isApmapPatch));
    expect(patches.map((patch) => [patch.width, patch.height])).toEqual([[3, 3], [5, 3], [3, 5]]);
    const matrices = faces(document).map((face) => face.projection);
    const text = mapTextFromApmap(parseApmapText(canonicalApmapText(document)));
    expect(text.match(/patchDef2/g)).toHaveLength(3);
    const back = apmapFromQuake3Map(parseQuake3Map(text), { sourceMap: 'q3-brushdef-patches.map', sourceSha256: sha256Hex(fixture('q3-brushdef-patches.map')) }).document;
    expect(back.entities.flatMap((entity) => entity.content.filter(isApmapPatch)).map((patch) => patch.control_points))
      .toEqual(patches.map((patch) => patch.control_points));
    expect(faces(back).map((face) => face.projection)).toEqual(matrices);
  });

  it('stores shader names without a textures/ prefix and writes them bare, as Q3Map2 prepends its own', () => {
    const { document, summary } = importOf('q3-classic-patches.map');
    expect(summary.prefixedShaders).toBeGreaterThan(0);
    expect(faces(document).some((face) => face.texture.startsWith('textures/'))).toBe(false);
    const text = mapTextFromApmap(document);
    expect(text).toContain(' mapio/crate 4 -12 30 0.25 0.75 0 0 0');
    expect(text).not.toContain('textures/');
  });

  it('accepts a file mixing brush syntaxes and refuses to export it whole — the documented asymmetry', () => {
    const { document, summary } = importOf('q3-mixed.map');
    expect(summary).toMatchObject({ classicBrushes: 3, brushPrimitiveBrushes: 1, patches: 3 });
    expect(() => mapTextFromApmap(document)).toThrow(MapExportError);
  });

  it('lists every plane that bounds nothing, and reports an exact duplicate as a risk, never dropping a face silently', () => {
    expect(importOf('q3-redundant-plane-unbounding.map').summary).toMatchObject({ redundantPlanes: 1, redundantPlaneBrushes: [{ entity: 0, brush: 0, faces: [1] }] });
    expect(importOf('q3-redundant-plane.map').summary).toMatchObject({ redundantPlanes: 0, risks: { duplicate_plane: 1 } });
  });

  it.each([['q3-refuse-patchdef3.map', 'Q3_DOOM3_GRAMMAR_UNSUPPORTED'], ['q3-refuse-brushdef3.map', 'Q3_DOOM3_GRAMMAR_UNSUPPORTED'],
    ['q3-refuse-patch-size.map', 'Q3_PATCH_SIZE_UNSUPPORTED']])('refuses %s whole (%s)', (name, code) => {
    const parsed = parseQuake3Map(fixture(name));
    expect(parsed.errors.map((error) => error.code)).toContain(code);
    expect(() => apmapFromQuake3Map(parsed, { sourceMap: name, sourceSha256: sha256Hex(fixture(name)) })).toThrow(Quake3ImportError);
  });

  it('async read and encode equal the synchronous ones, report progress, and a cancellation returns no document', async () => {
    const text = fixture('q3-classic-patches.map');
    const options = { sourceMap: 'q3-classic-patches.map', sourceSha256: sha256Hex(text) };
    const progress: number[] = [];
    const parsed = await parseQuake3MapAsync(text, { sliceMs: 0, yieldTo: async () => {}, onProgress: (done) => progress.push(done) });
    expect(parsed).toEqual(parseQuake3Map(text));
    const result = await apmapFromQuake3MapAsync(parsed, { ...options, sliceMs: 0, yieldTo: async () => {}, onProgress: (done) => progress.push(done) });
    expect(result).toEqual(apmapFromQuake3Map(parsed, options));
    expect(progress.length).toBeGreaterThan(1);
    const controller = new AbortController();
    await expect(apmapFromQuake3MapAsync(parsed, { ...options, signal: controller.signal, sliceMs: 0, yieldTo: async () => { controller.abort(); } }))
      .rejects.toMatchObject({ name: 'AbortError' });
    await expect(parseQuake3MapAsync(text, { signal: AbortSignal.abort() })).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('refuses a patch in a document that is not Quake III', () => {
    const { document } = importOf('q3-classic-patches.map');
    expect(() => mapTextFromApmap({ ...document, game: 'quake1' })).toThrow(MapExportError);
  });
});

describe('decode door', () => {
  it('refuses a version the contract does not hold, by type, before reading anything else', () => {
    expect(() => parseApmapText(JSON.stringify({ apmap_version: '999.999', entities: [] }))).toThrow(ApmapDecodeError);
  });
});
