/**
 * Decode an APMap 1.0 document into the parsed model the geometry worker already consumes.
 *
 * This is a **decoder, not a converter**. No `.map` text is produced here or anywhere else in the
 * browser: the document's own planes, texture names and projections are read straight into the
 * same `ParsedEntity` / `ParsedBrush` / `ParsedFace` shapes `parseQuakeMap` produces, and the one
 * convex-winding kernel reconstructs both formats. The APMap document remains the source document;
 * the scene the worker builds from it is a derived view.
 *
 * The one thing APMap adds to the parsed model is **identity**. A `.map` face has no name, so the
 * worker synthesises `e0-b1-f3` from array positions; an APMap face carries `fac_…`, which is
 * stable across a move, a re-export and a regeneration. `stableId` is therefore carried on each
 * parsed object and the worker prefers it, so scene, selection, annotation and cache identity are
 * the document's own ids rather than positions in an array.
 */
import { issueValue, type ApmapIssue, type ApmapIssueStage } from '../issues.ts';
import { ApmapDecodeError, currentApmapVersion, isApmapBrush, isApmapReadable, unsupportedApmapMessage, type ApmapBrushItem, type ApmapDocument, type ApmapEntity, type ApmapFace, type ApmapProjection } from './types.ts';

import { brushPrimitivesCovectors } from '../geometry/brushPrimitives.ts';
import { classicReaderOf, type ClassicReader } from '../geometry/projection.ts';
import type { FaceMapping, ParsedBrush, ParsedEntity, ParsedFace, ParsedMap, ParsedProperty, SourceRange } from '../map/types.ts';

const fail = (code: string, message: string, rule = ''): never => { throw new ApmapDecodeError(code, message, rule); };
const finite = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value);
const vec = (value: unknown, length: number): number[] | null =>
  Array.isArray(value) && value.length === length && value.every(finite) ? (value as number[]) : null;

/** APMap has no source line. The object path is what a diagnostic can actually act on. */
const range = (path: string): SourceRange => ({ line: 0, column: 0, excerpt: path });

function mapping(projection: ApmapProjection, path: string, points: readonly (readonly number[])[], reader: ClassicReader | undefined): FaceMapping {
  // APMap 1.5's third projection (`Q3_002`). Its matrix is read against the face's own plane points,
  // because the axis base it is written in depends on which way Q3Map2's normal points — so it is
  // resolved here, once, and never from a normal somebody else computed.
  if (projection?.mode === 'brush_primitives') {
    const matrix = projection.matrix;
    if (!Array.isArray(matrix) || matrix.length !== 2 || !matrix.every((row) => vec(row, 3)))
      return fail('apmap_face_invalid', `${path}: brush_primitives matrix must be two rows of three finite numbers`, 'SCH-Q-7');
    const mapped = faceMappingOf(projection, points, reader);
    return mapped.texture_axes ? mapped : fail('apmap_face_invalid', `${path}: a brushDef face needs three non-collinear plane points to place its texture`, 'SCH-7');
  }
  const scale = vec(projection?.scale, 2) ?? fail('apmap_face_invalid', `${path}: projection.scale must be two finite numbers`, 'SCH-14');
  const rotation = finite(projection?.rotation) ? projection.rotation : fail('apmap_face_invalid', `${path}: projection.rotation must be a finite number`, 'SCH-14');
  if (projection.mode === 'valve220') {
    vec(projection.u_axis, 4) ?? fail('apmap_face_invalid', `${path}: valve220 u_axis must be four finite numbers`, 'SCH-8');
    vec(projection.v_axis, 4) ?? fail('apmap_face_invalid', `${path}: valve220 v_axis must be four finite numbers`, 'SCH-8');
    return faceMappingOf(projection, undefined, reader);
  }
  if (projection.mode !== 'classic') return fail('apmap_face_invalid', `${path}: projection.mode must be 'classic', 'valve220' or 'brush_primitives'`, 'SCH-9');
  vec(projection.shift, 2) ?? fail('apmap_face_invalid', `${path}: classic projection.shift must be two finite numbers`, 'SCH-9');
  void scale; void rotation;
  return faceMappingOf(projection, undefined, reader);
}

/**
 * An APMap projection as the renderer's `FaceMapping` — **validation already done**.
 *
 * Split out of `mapping` above by `AUP 137`, which needed the same conversion after a *direct UV
 * edit*: the pane rewrites one face's `projection` in the document and the scene's copy of that face
 * has to follow, or the viewport keeps drawing the projection the reader has just replaced. A second
 * implementation of the conversion would agree on the day it was written and disagree the first time
 * either was touched — `projection.ts`'s own warning — so `mapping` now validates and delegates, and
 * this is the one rule.
 *
 * `[x, y, z, shift]`. The renderer keeps orientation (the axis) and offset (the shift) apart,
 * exactly as the classic branch does, so a Valve 220 face tiles in world space unchanged.
 *
 * A brushDef face (`Q3_002`) needs its `points`: without them its matrix cannot be placed, and the
 * mapping carries no `texture_axes` — which the renderer refuses by name rather than drawing a guess.
 */
export function faceMappingOf(projection: ApmapProjection, points: readonly (readonly number[])[] | undefined, reader: ClassicReader | undefined): FaceMapping {
  if (projection.mode === 'brush_primitives') {
    const covectors = points ? brushPrimitivesCovectors(projection.matrix, points) : undefined;
    const axis = (one: { gradient: number[]; shift: number }) => ({ x: one.gradient[0], y: one.gradient[1], z: one.gradient[2], shift: one.shift });
    return { mode: 'brush_primitives', shift: { x: 0, y: 0 }, rotation: 0, scale: { x: 1, y: 1 },
             matrix: [[...projection.matrix[0]], [...projection.matrix[1]]] as [[number, number, number], [number, number, number]],
             ...(covectors ? { texture_axes: { u: axis(covectors.u), v: axis(covectors.v) } } : {}) };
  }
  const rotation = projection.rotation ?? 0;
  const scale = { x: projection.scale[0], y: projection.scale[1] };
  if (projection.mode === 'valve220') {
    const u = projection.u_axis, v = projection.v_axis;
    return { mode: 'valve220', shift: { x: u[3], y: v[3] }, rotation, scale,
             valve_220_axes: { u: { x: u[0], y: u[1], z: u[2], shift: u[3] }, v: { x: v[0], y: v[1], z: v[2], shift: v[3] } } };
  }
  // `NEW_325B`: only a Quake III document's Classic face says whose base-axis rule reads it, so every
  // other mapping object is exactly what it was.
  return { mode: 'classic', shift: { x: projection.shift[0], y: projection.shift[1] }, rotation, scale,
           ...(reader === 'quake3' ? { classic_reader: 'quake3' as const } : {}) };
}

function face(source: ApmapFace, index: number, path: string, reader: ClassicReader | undefined): ParsedFace {
  const id = typeof source?.face_id === 'string' && source.face_id ? source.face_id : fail('apmap_face_invalid', `${path}: face_id is required`, 'SCH-14');
  const points = source?.plane?.points;
  if (!Array.isArray(points) || points.length !== 3) fail('apmap_face_invalid', `${path}: plane.points must be exactly three points`, 'SCH-7');
  const decoded = points.map((point) => vec(point, 3) ?? fail('apmap_face_invalid', `${path}: a plane point is not three finite numbers`, 'SCH-7'));
  const texture = typeof source.texture === 'string' ? source.texture : fail('apmap_face_invalid', `${path}: texture is required`, 'SCH-14');
  return {
    index, stableId: id, points: decoded as ParsedFace['points'], textureName: texture,
    mapping: mapping(source.projection, path, decoded, reader), range: range(path),
    // Quake 2 numeric face values round-trip verbatim and 1.0 assigns them no meaning; the parsed
    // model already carries them as opaque strings, so they travel the same way here.
    tail: Array.isArray(source.tail) ? source.tail.map(String) : [],
  };
}

/**
 * One APMap brush as the geometry kernel's input.
 *
 * Lifted out of `entity` and exported for the editing operations: `previewFacePush` reconstructs
 * and convexity-checks a brush it has just modified, and it must decode that brush **the same way
 * a load does** or the preview would be checking a slightly different object from the one the
 * renderer will draw. `path` is the diagnostic address (`e0.b3`), which APMap has instead of a
 * source line.
 */
export function parsedBrushFromApmap(item: ApmapBrushItem, index: number, path: string, reader?: ClassicReader): ParsedBrush {
  const brushId = typeof item.brush_id === 'string' && item.brush_id ? item.brush_id : fail('apmap_brush_invalid', `${path}: brush_id is required`, 'SCH-10');
  const faces = Array.isArray(item.faces) ? item.faces : fail('apmap_brush_invalid', `${path}: faces must be an array`, 'SCH-6');
  if (faces.length < 4) fail('apmap_brush_invalid', `${path}: a brush needs at least four faces`, 'SCH-6');
  return { index, stableId: brushId, range: range(path),
           faces: faces.map((face_, position) => face(face_, position, `${path}.f${position}`, reader)) };
}

function entity(source: ApmapEntity, index: number, reader: ClassicReader): ParsedEntity {
  const id = typeof source?.entity_id === 'string' && source.entity_id ? source.entity_id : fail('apmap_entity_invalid', `e${index}: entity_id is required`, 'SCH-10');
  const content = Array.isArray(source?.content) ? source.content : fail('apmap_entity_invalid', `e${index}: content must be an array`, 'SCH-12');
  // No prototype (`Q2_005A`): a key spelled `__proto__` or `constructor` is read as the data it is.
  const properties: Record<string, string> = Object.create(null);
  // Every property line in content order, as `parseQuakeMap` keeps them (`Q2_002`): the scene derives the position
  // of each key's last occurrence from these, which the flattened `properties` cannot hold (`Q2_005A`).
  const entries: ParsedProperty[] = [];
  const brushes: ParsedBrush[] = [];
  for (const item of content) {
    if (isApmapBrush(item)) {
      brushes.push(parsedBrushFromApmap(item, brushes.length, `e${index}.b${brushes.length}`, reader));
      continue;
    }
    // APMap 1.5: a Quake III patch is carried in the DOCUMENT and not drawn. The scene is a derived
    // view and the document stays the source (see the module note), so skipping it here is what
    // keeps it: nothing downstream of the scene writes a document, and the editor's surface hold
    // refuses every edit while a patch is present (`quake3Surfaces.ts`).
    if (item?.kind === 'patch') continue;
    if (item?.kind !== 'property') fail('apmap_entity_invalid', `e${index}: content item kind must be 'property', 'brush' or 'patch'`, 'SCH-12');
    // A repeated key is legal in APMap and in `.map`. The flattened property map keeps the last
    // value, which is what `parseQuakeMap` does, so both formats answer a lookup identically. The
    // ordered content itself is not the renderer's concern — it matters on export, which reads the
    // ordered content directly (`domain/mapBundleExport.ts`).
    properties[String(item.key)] = String(item.value);
    entries.push({ key: String(item.key), value: String(item.value), afterBrushes: brushes.length });
  }
  return { index, stableId: id, properties, entries, brushes, range: range(`e${index}`) };
}

/**
 * Promote a document to the current APMap version. Deterministic, and the ONLY normalization here.
 *
 * 1.1 was built additively from 1.0 — one new `derived_from` kind and one optional brush member,
 * nothing an older version defined changed — so promotion is the header plus whatever structural
 * defaults the current contract requires and the old one had no place for. Today that is exactly
 * one member: `groups`, which 1.2 requires of every writer. That is the whole reason the working
 * state can be current without a conversion pass, a migration wizard, or any risk to what the
 * document actually says.
 *
 * What it must not touch, and does not: `document_id`, `revision`, every `entity_id`/`brush_id`/
 * `face_id`, every plane point and projection, every `derived_from` record, `provenance`,
 * `relationships`, `extensions`. A shallow copy is exactly right for that — one top-level member is
 * replaced and every other value is the SAME object, so there is no deep clone to get subtly wrong
 * and nothing can be re-serialized into a different shape on the way through.
 *
 * A document already at the current version is returned unchanged, by identity.
 */
export function promoteApmapDocument<T extends { apmap_version: string; $schema?: string; groups?: unknown }>(document: T): T {
  const current = currentApmapVersion();
  // A document already current still needs `groups` if it somehow lacks it — a hand-written
  // fixture, or a producer that declared the version without writing the member. Returning it by
  // identity when it IS complete is what keeps the common path free.
  if (document.apmap_version === current) return Array.isArray(document.groups) ? document : { ...document, groups: [] };
  const promoted: T = { ...document, apmap_version: current };
  // The structural default the current contract requires. A legacy document could not carry
  // groups, so the empty array is the truthful answer, not a guess — and supplying it here is what
  // lets everything downstream treat `groups` as present without each caller re-checking.
  if (!Array.isArray(promoted.groups)) (promoted as { groups?: unknown }).groups = [];
  // `$schema` NAMES A VERSION, so it is part of the header and moves with it.
  //
  // Missing this produced a stored document reading `"apmap_version": "1.1"` beside
  // `"$schema": ".../schemas/apmap/1.0/apmap.schema.json"` — a file that contradicts itself about
  // which contract it was written against. It was found by saving a legacy map in a browser, not
  // by a test, because every test compared the field that had been remembered.
  //
  // The URL is rewritten only where it is recognisably an APMap contract URL carrying a version
  // segment; anything else a producer put there is left alone rather than guessed at.
  if (typeof promoted.$schema === 'string')
    promoted.$schema = promoted.$schema.replace(/(\/schemas\/apmap\/)\d+\.\d+(\/)/, `$1${current}$2`);
  return promoted;
}

/**
 * Structural decode of an already-parsed JSON value. Throws `ApmapDecodeError` on refusal.
 *
 * A readable legacy document is decoded and then PROMOTED, so what leaves this function is always
 * current. That is the working-state rule in one place: everything downstream — the scene, the
 * editing kernel, save, export, the collaboration payload — receives a current document and cannot
 * accidentally propagate a legacy one, because there is no legacy one after this line.
 */
export function decodeApmapDocument(value: unknown): ApmapDocument {
  if (!value || typeof value !== 'object' || Array.isArray(value)) fail('apmap_malformed', 'APMap document must be a JSON object');
  const document = value as ApmapDocument;
  const version = document.apmap_version;
  if (typeof version !== 'string' || !version) fail('apmap_version_missing', 'APMap document does not declare apmap_version', 'SCH-2');
  // The version gate, before anything structural: membership of the loaded contract bundle. Still
  // no hand-maintained list — the set is the schema files the backend found on disk — and a version
  // outside it is refused here rather than reinterpreted as something it is not.
  if (!isApmapReadable(version)) fail('apmap_version_unsupported', unsupportedApmapMessage(version), 'SCH-1');
  if (!Array.isArray(document.entities)) fail('apmap_malformed', 'APMap entities must be an array', 'SCH-2');
  if (!document.frame || typeof document.frame !== 'object') fail('apmap_malformed', 'APMap frame is required', 'SCH-2');
  return promoteApmapDocument(document);
}

/** Parse APMap text. `JSON.parse` refuses `NaN` and `Infinity` outright, which is SEM-11 for free. */
export function parseApmapText(text: string): ApmapDocument {
  let value: unknown;
  try { value = JSON.parse(text); } catch (error) { fail('apmap_malformed', `APMap document is not valid JSON: ${(error as Error).message}`); }
  return decodeApmapDocument(value);
}

/**
 * A refusal from the decoder, as one more issue on the pipeline's list.
 *
 * The decoder is the last stage in practice — it is what actually stops a document from reaching
 * the kernel — so its refusal belongs beside the collected issues rather than replacing them. The
 * stage is read from the code, because `apmap_malformed` is a JSON-level fault while a missing
 * `face_id` is a render-readiness one.
 */
export function issueFromDecodeError(error: ApmapDecodeError): ApmapIssue {
  const stage: ApmapIssueStage = error.code === 'apmap_malformed' ? 'json-parse' : error.code.startsWith('apmap_version') ? 'schema' : 'geometry';
  return { stage, severity: 'fatal', message: error.message, path: null, value: issueValue(error.rule || error.code) };
}

/** The document as the geometry kernel's input. Source order and indices are preserved exactly. */
export function parsedMapFromApmap(document: ApmapDocument): ParsedMap {
  const reader = classicReaderOf(document);
  return { entities: document.entities.map((source, index) => entity(source, index, reader)), errors: [] };
}

/** `frame.to_source` as a translation, or `null` when it is a shape this build cannot apply. */
export function toSourceTranslation(document: ApmapDocument): [number, number, number] | null {
  const transform = document.frame?.to_source;
  if (transform?.type === 'identity') return [0, 0, 0];
  if (transform?.type !== 'translation') return null;
  const translation = vec(transform.translation, 3);
  return translation ? (translation as [number, number, number]) : null;
}

/** The document identity a derived scene records, without the geometry it is the derived view of. */
export const apmapSourceIdentity = (document: ApmapDocument) => ({
  geometry_format: 'apmap' as const, apmap_version: document.apmap_version, document_id: document.document_id,
  role: document.role, revision: document.revision, id_policy: document.id_policy,
  game: document.game, map_dialect: document.map_dialect, frame: document.frame,
  relationship_count: document.relationships?.length ?? 0,
});
