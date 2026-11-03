/**
 * **A Quake III `.map` as an APMap 1.5 document** — `Q3_002`, the import half of the Quake III codec.
 *
 * `encode.ts` turns the Quake 1 parsed model into a document; this does the same for
 * `map/quake3.ts`'s reading, and it differs where Quake III differs:
 *
 * - **`game: quake3`, `map_dialect: quake3_extended`** — the one pair SEMANTICS §2 gives the Quake
 *   III grammar, and the only pair in which a patch or a brushDef face is legal (SEM-Q-4).
 * - **Content keeps its order.** The Quake III reader returns each entity's properties, brushes and
 *   patches in the order the file wrote them, so a key written after a brush stays after it — the
 *   loss `encode.ts` documents for Quake 1 does not happen here.
 * - **A brushDef face keeps its matrix verbatim** (SER-7Q) and a patch keeps every control-point
 *   number verbatim; a classic face's five numbers follow SER-7 exactly as a Quake 1 face's do.
 * - **Shader names carry no `textures/` prefix.** Q3Map2 prepends it (measured: a prefixed name
 *   compiles as `textures/textures/…`), and SEMANTICS §9c stores the name "as a face's texture is".
 *   A name the file wrote WITH the prefix is stored without it and counted, so the editor can say
 *   how many it corrected rather than correcting silently.
 * - **Every brush crosses `brushLegality` before it is added**, and the import is refused whole —
 *   naming each brush by entity, brush and source line — when one is impossible: a non-finite
 *   number, collinear plane points, no bounded volume, an inside-out or mixed winding, or a
 *   brushDef matrix with determinant 0 (SEM-Q-3). Nothing is imported as "the brushes that were fine".
 * - **A plane that bounds nothing is left out, and reported** (`Q3_012B`, DESIGN §204 rule 5). A
 *   brush whose other planes already describe a sound convex solid is that solid: Q3Map2 2.5.17n
 *   compiles it without a line (measured — the idle plane becomes a brush side with no winding and
 *   no surface), and six real sources were refused whole over one. The face is not stored, the
 *   summary names it by count and by entity, brush, line and face index, and the faces that stay
 *   keep the ids their positions in the FILE give them. A brush that is not a solid without those
 *   planes is refused exactly as before.
 *
 * Identity is `encode.ts`'s: `source_map` derivation over the file's own name and digest, at
 * `e<n>`, `e<n>.b<n>`, `e<n>.b<n>.f<n>` and — 1.5 — `e<n>.p<n>`, where brushes and patches are
 * counted separately so adding a patch never renumbers a brush (SEMANTICS §3.5).
 */
import type { Quake3Brush, Quake3Face, Quake3ParsedMap, Quake3Patch } from '../map/quake3.ts';
import { brushLegality, type BrushLegalityCode } from '../geometry/brushLegality.ts';
import { brushPrimitivesDeterminant } from '../geometry/brushPrimitives.ts';
import { sha256Hex } from '../hash/sha256.ts';
import { ApmapEncodeError, canonicalApmapNumber, exactApmapNumber } from './canonical.ts';
import { APMAP_ENCODER } from './encode.ts';
import { bareSourceMapName, deriveApmapId, sourceMapDerivedFrom, type ApmapDerivedFrom, type ApmapObjectKind } from './identity.ts';
import { currentApmapVersion } from './types.ts';
import { APMAP_DIALECT_VALVE220 } from './dialect.ts';
import type { ApmapBrushItem, ApmapContentItem, ApmapDocument, ApmapEntity, ApmapFace, ApmapPatchItem, ApmapVec3 } from './types.ts';

import { QUAKE3_GAME_NAME, QUAKE3_MAP_DIALECT, quake3MapShaderName } from './quake3Names.ts';
export { QUAKE3_GAME_NAME, QUAKE3_MAP_DIALECT, QUAKE3_SHADER_PREFIX, quake3MapShaderName } from './quake3Names.ts';

/** The legality codes that make a brush impossible rather than merely risky. The import refuses on these. */
/**
 * `texture_basis_degenerate` is NOT here, and that was measured: a real Quake III source (`Q3_002`'s
 * corpus campaign) has hundreds of Valve 220 faces with parallel axes and Q3Map2 compiles it — the
 * texture streaks, the solid is fine. A degenerate texture is a risk, counted. The one degenerate
 * texture that IS refused is a brushDef matrix with determinant 0, because SEM-Q-3 makes that
 * document invalid; `brush()` below checks it by name.
 */
export const QUAKE3_IMPORT_REFUSED_CODES: ReadonlySet<BrushLegalityCode> = new Set<BrushLegalityCode>([
  'non_finite', 'unserializable', 'not_convex', 'winding_inward', 'winding_mixed',
]);

/** One brush the import refused, by address and line. Never its numbers or its shader names. */
export type Quake3RejectedBrush = { entity: number; brush: number; line: number; codes: BrushLegalityCode[] };

/**
 * One brush imported WITHOUT some of the planes its source declares: `faces` are the indices, in the
 * file's own order, of the planes that bound no part of the solid. An address, never content.
 */
export type Quake3RedundantPlaneBrush = { entity: number; brush: number; line: number; faces: number[] };

export class Quake3ImportError extends Error {
  constructor(readonly code: 'quake3_import_parse' | 'quake3_import_brush_illegal' | 'quake3_import_empty',
              message: string, readonly detail: { parseErrors?: Quake3ParsedMap['errors']; rejected?: Quake3RejectedBrush[] } = {}) {
    super(message);
    this.name = 'Quake3ImportError';
  }
}

export type Quake3EncodeOptions = {
  /** The source file's name, not its path. An identity input. */
  sourceMap: string;
  /** SHA-256 of the file's bytes, lowercase hex. An identity input. */
  sourceSha256: string;
  documentId?: string;
};

/** What the import did, in counts. What the editor shows and the handoff records — no map content. */
export type Quake3ImportSummary = {
  entities: number; properties: number; brushes: number; classicBrushes: number; valve220Brushes: number; brushPrimitiveBrushes: number;
  faces: number; patches: number;
  /** Shader names the file wrote with a `textures/` prefix, stored without it. */
  prefixedShaders: number;
  /** Distinct shader names, after the prefix rule. */
  shaders: number;
  /** Risk findings that did NOT refuse the import (`thin`, `duplicate_plane`, …), by code. */
  risks: Record<string, number>;
  /** Planes the source declares that bound nothing, left out of the document. `faces` counts what was stored. */
  redundantPlanes: number;
  /** Every brush that lost one, by address — the whole list, so nothing was left out silently. */
  redundantPlaneBrushes: Quake3RedundantPlaneBrush[];
};

export type Quake3ImportResult = { document: ApmapDocument; summary: Quake3ImportSummary };

const DOCUMENT_ID = /^[0-9A-Za-z][0-9A-Za-z._:-]{0,127}$/;
const MAP_STRING = /^[^"\r\n]*$/;
const MAX_PATCH_COORDINATE = 131072;
const MAX_MATRIX_COEFFICIENT = 1e6;

const documentIdFor = (sourceMap: string): string => {
  const stem = sourceMap.replace(/\.map$/i, '').replace(/[^0-9A-Za-z._-]+/g, '-').replace(/^[^0-9A-Za-z]+/, '');
  return `${stem || 'map'}:full_map`.slice(0, 128);
};

/**
 * The document for a parsed Quake III map. Throws `Quake3ImportError` — never returns a document
 * built from part of the file — when the reader recorded any error or any brush is impossible.
 */
export function apmapFromQuake3Map(parsed: Quake3ParsedMap, options: Quake3EncodeOptions): Quake3ImportResult {
  const steps = encode(parsed, options);
  for (let step = steps.next(); ; step = steps.next()) if (step.done) return step.value;
}

/**
 * The same encoding, sliced — `Q3_002`'s large-import rule. Every brush crosses `brushLegality`, which
 * is most of an import's time (≈ 0.5 ms a brush measured, so 20 000 brushes would hold the thread for
 * ten seconds), so it yields every `sliceMs`, reports brushes done of brushes total, and stops at the
 * first check after its signal fires with the DOM's `AbortError`. Nothing partial is ever returned.
 */
export async function apmapFromQuake3MapAsync(parsed: Quake3ParsedMap, options: Quake3EncodeOptions & {
  signal?: AbortSignal; onProgress?: (done: number, total: number) => void; sliceMs?: number; yieldTo?: () => Promise<void>;
}): Promise<Quake3ImportResult> {
  const slice = options.sliceMs ?? 12;
  const pause = options.yieldTo ?? (() => new Promise<void>((resolve) => setTimeout(resolve, 0)));
  const now = () => (typeof performance !== 'undefined' ? performance.now() : Date.now());
  const aborted = () => (typeof DOMException === 'function' ? new DOMException('Quake III map import cancelled', 'AbortError')
    : Object.assign(new Error('Quake III map import cancelled'), { name: 'AbortError' }));
  if (options.signal?.aborted) throw aborted();
  const steps = encode(parsed, options);
  let started = now();
  for (let step = steps.next(); ; step = steps.next()) {
    if (step.done) { options.onProgress?.(parsed.stats.brushes, parsed.stats.brushes); return step.value; }
    if (now() - started < slice) continue;
    options.onProgress?.(step.value, parsed.stats.brushes);
    await pause();
    if (options.signal?.aborted) throw aborted();
    started = now();
  }
}

/** The encoder as a generator: it yields the number of brushes checked so far, and returns the result. */
function* encode(parsed: Quake3ParsedMap, options: Quake3EncodeOptions): Generator<number, Quake3ImportResult, void> {
  if (parsed.errors.length)
    throw new Quake3ImportError('quake3_import_parse',
      `The Quake III map could not be read: ${parsed.errors.length} error${parsed.errors.length === 1 ? '' : 's'}, the first at line ${parsed.errors[0].range.line}, column ${parsed.errors[0].range.column} (${parsed.errors[0].code}): ${parsed.errors[0].message}. Nothing was imported.`,
      { parseErrors: parsed.errors });
  if (!parsed.entities.length) throw new Quake3ImportError('quake3_import_empty', 'The file holds no entities, so there is nothing to import.');

  const sourceMap = bareSourceMapName(options.sourceMap);
  if (!/^[0-9a-f]{64}$/.test(options.sourceSha256))
    throw new ApmapEncodeError('apmap_encode_source_sha256', 'sourceSha256 is not a lowercase hex SHA-256 digest', null, 'SCH-2');
  const documentId = options.documentId ?? documentIdFor(sourceMap ?? 'map');
  if (!DOCUMENT_ID.test(documentId)) throw new ApmapEncodeError('apmap_encode_document_id', `document_id ${JSON.stringify(documentId)} is not valid`, null, 'SCH-2');

  const identity = (kind: ApmapObjectKind, sourcePath: string): { id: string; derived_from: ApmapDerivedFrom } => {
    const derived = sourceMapDerivedFrom(sourcePath, sourceMap, options.sourceSha256);
    return { id: deriveApmapId(kind, derived), derived_from: derived };
  };

  const summary: Quake3ImportSummary = { entities: 0, properties: 0, brushes: 0, classicBrushes: 0, valve220Brushes: 0, brushPrimitiveBrushes: 0,
                                         faces: 0, patches: 0, prefixedShaders: 0, shaders: 0, risks: {},
                                         redundantPlanes: 0, redundantPlaneBrushes: [] };
  const shaderNames = new Set<string>();
  const storedShader = (name: string, path: string): string => {
    const stored = quake3MapShaderName(name);
    if (!/^[^\s"]+$/.test(stored))
      throw new ApmapEncodeError('apmap_encode_texture_invalid', `${path}: the shader name contains whitespace or a quote`, path, 'SCH-14');
    return stored;
  };
  /** Counted only for what the document keeps: a plane left out names no shader of this map. */
  const countShader = (name: string, stored: string) => {
    if (stored !== name) summary.prefixedShaders += 1;
    shaderNames.add(stored);
  };
  const shader = (name: string, path: string): string => {
    const stored = storedShader(name, path);
    countShader(name, stored);
    return stored;
  };

  const face = (source: Quake3Face, path: string): ApmapFace => {
    const { id, derived_from } = identity('face', path);
    const points = source.points.map((point) => point.map((value) => canonicalApmapNumber(value, path, 'plane.points')) as ApmapVec3);
    const texture = storedShader(source.texture, path);
    const n = (value: number, member: string) => canonicalApmapNumber(value, path, member);
    const projection: ApmapFace['projection'] = source.valve
      ? { mode: 'valve220', u_axis: source.valve.u.map((value) => n(value, 'u_axis')) as [number, number, number, number],
          v_axis: source.valve.v.map((value) => n(value, 'v_axis')) as [number, number, number, number],
          rotation: n(source.valve.rotation, 'rotation'), scale: [n(source.valve.scale[0], 'scale'), n(source.valve.scale[1], 'scale')] }
      : source.matrix
      ? { mode: 'brush_primitives', matrix: source.matrix.map((row) => row.map((value) => {
          if (Math.abs(value) > MAX_MATRIX_COEFFICIENT)
            throw new ApmapEncodeError('apmap_encode_matrix_range', `${path}: a texture matrix coefficient is outside ±${MAX_MATRIX_COEFFICIENT}`, path, 'SCH-Q-7');
          return exactApmapNumber(value, path);
        })) as [[number, number, number], [number, number, number]] }
      : { mode: 'classic',
          shift: [canonicalApmapNumber(source.classic!.shift[0], path, 'shift'), canonicalApmapNumber(source.classic!.shift[1], path, 'shift')],
          rotation: canonicalApmapNumber(source.classic!.rotation, path, 'rotation'),
          scale: [canonicalApmapNumber(source.classic!.scale[0], path, 'scale'), canonicalApmapNumber(source.classic!.scale[1], path, 'scale')] };
    const tail = source.tail.length ? source.tail.map((value) => canonicalApmapNumber(value, path, 'tail')) : undefined;
    // SER-5: face_id, derived_from, plane, texture, projection, tail.
    return { face_id: id, derived_from, plane: { points: points as ApmapFace['plane']['points'] }, texture, projection, ...(tail ? { tail } : {}) } as ApmapFace;
  };

  const rejected: Quake3RejectedBrush[] = [];
  const brush = (source: Quake3Brush, entityIndex: number, brushIndex: number): ApmapBrushItem => {
    const path = `e${entityIndex}.b${brushIndex}`;
    const { id, derived_from } = identity('brush', path);
    let item: ApmapBrushItem = { kind: 'brush', brush_id: id, derived_from, faces: source.faces.map((one, index) => face(one, `${path}.f${index}`)) };
    let legality = brushLegality(item, { context: path });
    let kept = source.faces;
    // `Q3_012B`: the planes convexity names as carrying no facet are tried WITHOUT — once, because a
    // plane with fewer than three of the solid's corners on it contains the whole solid in its
    // half-space, so removing it cannot move a corner. What is left is judged from scratch by the
    // same contract; only a brush that then passes is imported, and it is imported as what passed.
    const idle = legality.convexity.reason === 'not_convex' ? new Set(legality.convexity.faces) : null;
    if (idle?.size) {
      const candidate: ApmapBrushItem = { ...item, faces: item.faces.filter((_one, index) => !idle.has(index)) };
      const without = brushLegality(candidate, { context: path });
      if (!without.codes.some((code) => QUAKE3_IMPORT_REFUSED_CODES.has(code))) {
        kept = source.faces.filter((_one, index) => !idle.has(index));
        item = candidate;
        legality = without;
        summary.redundantPlanes += idle.size;
        summary.redundantPlaneBrushes.push({ entity: entityIndex, brush: brushIndex, line: source.line, faces: [...idle].sort((a, b) => a - b) });
      }
    }
    for (const [index, one] of kept.entries()) countShader(one.texture, item.faces[index].texture);
    const refused = legality.codes.filter((code) => QUAKE3_IMPORT_REFUSED_CODES.has(code));
    // SEM-Q-3 is a document rule as well as a legality one: a degenerate matrix is refused even when
    // the solid itself is fine, because the document it would produce is invalid.
    if (source.syntax === 'brush_primitives' && item.faces.some((one) => one.projection.mode === 'brush_primitives' && brushPrimitivesDeterminant(one.projection.matrix) === 0)
        && !refused.includes('texture_basis_degenerate')) refused.push('texture_basis_degenerate');
    if (refused.length) rejected.push({ entity: entityIndex, brush: brushIndex, line: source.line, codes: [...new Set(refused)] });
    for (const finding of legality.findings)
      if (!QUAKE3_IMPORT_REFUSED_CODES.has(finding.code)) summary.risks[finding.code] = (summary.risks[finding.code] ?? 0) + 1;
    summary.brushes += 1;
    summary.faces += item.faces.length;
    if (source.syntax === 'classic') summary.classicBrushes += 1;
    else if (source.syntax === 'valve220') summary.valve220Brushes += 1;
    else summary.brushPrimitiveBrushes += 1;
    return item;
  };

  const patch = (source: Quake3Patch, entityIndex: number, patchIndex: number): ApmapPatchItem => {
    const path = `e${entityIndex}.p${patchIndex}`;
    const { id, derived_from } = identity('patch', path);
    const control_points = source.controlPoints.map((column) => column.map((point) => point.map((value) => {
      if (Math.abs(value) > MAX_PATCH_COORDINATE)
        throw new ApmapEncodeError('apmap_encode_patch_range', `${path}: a control point component is outside ±${MAX_PATCH_COORDINATE}`, path, 'SCH-Q-3');
      return exactApmapNumber(value, path);
    })));
    summary.patches += 1;
    // SER-5 for a patch item: kind, patch_id, derived_from, texture, width, height, control_points, tail.
    return { kind: 'patch', patch_id: id, derived_from, texture: shader(source.texture, path), width: source.width, height: source.height,
             control_points, tail: source.tail.map((value) => canonicalApmapNumber(value, path, 'tail')) };
  };

  const entities: ApmapEntity[] = [];
  let checked = 0;
  for (const [index, source] of parsed.entities.entries()) {
    const path = `e${index}`;
    const content: ApmapContentItem[] = [];
    let brushes = 0, patches = 0;
    for (const item of source.content) {
      if (item.kind === 'property') {
        if (!MAP_STRING.test(item.key) || !MAP_STRING.test(item.value))
          throw new ApmapEncodeError('apmap_encode_property_invalid', `${path}: a property contains a quote or a line break`, path, 'SCH-5');
        content.push({ kind: 'property', key: item.key, value: item.value });
        summary.properties += 1;
      } else if (item.kind === 'brush') { content.push(brush(item, index, brushes++)); yield ++checked; }
      else content.push(patch(item, index, patches++));
    }
    if (!content.length) throw new ApmapEncodeError('apmap_encode_entity_empty', `${path}: an entity with no properties, brushes or patches cannot be represented`, path, 'SCH-12');
    const { id, derived_from } = identity('entity', path);
    summary.entities += 1;
    entities.push({ entity_id: id, derived_from, content });
  }

  if (rejected.length) {
    const first = rejected.slice(0, 5).map((one) => `entity ${one.entity} brush ${one.brush} (line ${one.line}: ${one.codes.join(', ')})`).join('; ');
    // `Q3_012B`: a zero-determinant brushDef matrix is the one refusal here of a map Q3Map2 compiles
    // (measured: exit 0, no line about it, the face's texture coordinates collapse). The solid is
    // sound; what cannot be written is the matrix (SEM-Q-3). So the sentence says that, not "impossible".
    const matrixOnly = rejected.every((one) => one.codes.length === 1 && one.codes[0] === 'texture_basis_degenerate');
    throw new Quake3ImportError('quake3_import_brush_illegal',
      matrixOnly
        ? `${rejected.length} brushDef brush${rejected.length === 1 ? ' has' : 'es have'} a texture matrix with determinant 0 and the map was not imported: ${first}${rejected.length > 5 ? '; …' : ''}. Q3Map2 compiles such a face with its texture collapsed; an APMap document cannot hold the matrix (SEM-Q-3). Fit the texture on those faces in the source editor and import again.`
        : `${rejected.length} brush${rejected.length === 1 ? ' is' : 'es are'} impossible geometry and the map was not imported: ${first}${rejected.length > 5 ? '; …' : ''}.`,
      { rejected });
  }
  summary.shaders = shaderNames.size;

  const document = {
    $schema: `https://auto-pigeon.org/schemas/apmap/${currentApmapVersion()}/apmap.schema.json`,
    apmap_version: currentApmapVersion(),
    document_id: documentId,
    revision: 0,
    role: 'full_map',
    game: QUAKE3_GAME_NAME,
    // A map written in Valve 220 says so, so the editor authors its new faces in Valve 220 too
    // (`isValve220Document`); every other Quake III map is `quake3_extended`. SEM-Q-4 keys patches and
    // brushDef faces on `game`, so both stay legal either way.
    map_dialect: summary.valve220Brushes ? APMAP_DIALECT_VALVE220 : QUAKE3_MAP_DIALECT,
    id_policy: 'derived',
    frame: { space: 'source_world', units: 'quake_unit', coordinate_system: { handedness: 'right', up_axis: 'z' }, to_source: { type: 'identity' } },
    provenance: { ...APMAP_ENCODER, pipeline: ['quake3-map-import'],
                  source: { ...(sourceMap ? { map_name: sourceMap } : {}), map_sha256: options.sourceSha256 } },
    entities,
    groups: [],
  } as ApmapDocument;
  return { document, summary };
}

/** `sha256Hex` over the text, for callers that hold text rather than bytes (tests, the CLI). */
export const quake3SourceDigest = (text: string): string => sha256Hex(text);
