/**
 * `.map` → APMap. The import half of the codec.
 *
 * `parsedMapFromApmap` reads a document into the parsed model; this goes the other way, turning the
 * parsed model into a document whose every object has a stable, reproducible identity. Together they
 * are a codec, not a converter: **nothing in the loading path calls this.** Opening a `.map` still
 * renders `.map`, and opening an `.apmap` still renders `.apmap`. Encoding is an explicit act a
 * caller asks for — the first step of "import this map into a collaborative document".
 *
 * Two properties are worth stating plainly, because they are what make the output trustworthy:
 *
 * - **Deterministic.** Same `.map` bytes, same options, same document — down to every id. There is
 *   no clock, no counter and no randomness in here. `provenance.generated_at` is the one thing the
 *   format allows to break that (SER-8) and it is never emitted; a caller who wants it adds it.
 * - **Canonical by construction.** Every number is put through SER-7 (six decimal places, `-0` → 0)
 *   and every member is inserted in the specification's declared order (SER-5), so
 *   `JSON.stringify(document, null, 2) + '\n'` is already the canonical serialization — no separate
 *   writer to drift out of step with the encoder.
 *
 * Content order is the source's: `ParsedEntity.entries` keeps every property line, a repeated key
 * twice and a key written after a brush after it (`Q2_002`). A `properties` edited after the read is
 * reconciled into that order by the editor's policy, never substituted for it (`Q2_002A`); `properties`
 * alone is the order only for a parsed model whose reader kept none.
 */
import type { ParsedBrush, ParsedEntity, ParsedFace, ParsedMap } from '../map/types.ts';
import { sha256Hex } from '../hash/sha256.ts';
import { bareSourceMapName, deriveApmapId, sourceMapDerivedFrom, type ApmapDerivedFrom, type ApmapObjectKind } from './identity.ts';
import { currentApmapVersion } from './types.ts';
import type { ApmapBrushItem, ApmapContentItem, ApmapDocument, ApmapEntity, ApmapFace, ApmapFrame, ApmapGroup, ApmapProjection, ApmapRole, ApmapVec3 } from './types.ts';
import { ApmapEncodeError, canonicalApmapNumber, canonicalApmapText } from './canonical.ts';
import { faithfulClassicNumbers, type ClassicFidelity } from './classicFidelity.ts';
import { classicReaderOf } from '../geometry/projection.ts';
import { reconstructBrush } from '../geometry/reconstructBrush.ts';
import { applyPropertyEdits, lastWinsProperties, propertyEditsBetween } from './propertyEdits.ts';

// The serialization rules moved to `canonical.ts` when the canonical *writer* was added there:
// SER-7 and the error type belong beside the rest of SER-1 … SER-7 rather than beside the `.map`
// importer that happens to have needed them first. Re-exported so no consumer had to move.
export { ApmapEncodeError, canonicalApmapNumber } from './canonical.ts';

export type ApmapEncodeOptions = {
  /**
   * `document_id`. Defaults to `<source stem>:<role>`, so re-importing the same map reproduces it.
   * Required when no `sourceMap` is given: an identity nobody chose must not be invented.
   */
  documentId?: string;
  /** The source file's **name**, not its path — `e1m1.map`. A directory component is stripped. */
  sourceMap?: string;
  /** The source text, to hash. Ignored when `sourceSha256` is supplied. */
  sourceText?: string;
  /** The source map's SHA-256, if it is already known. An identity input (§3.5). */
  sourceSha256?: string;
  role?: ApmapRole;
  revision?: number;
  /** A registry name — `quake1`, `quake2`, `quake3`, `half_life`. */
  game?: string;
  /** Defaults to `valve_220` when any face carries Valve 220 axes, `quake_standard` otherwise. */
  mapDialect?: string;
  /**
   * `derived` (the default) records `derived_from` on every object, so any consumer can recompute
   * its id and check it. `minted` emits the same ids without the records — smaller, unverifiable.
   */
  idPolicy?: 'derived' | 'minted';
  frame?: ApmapFrame;
  /** `null` omits provenance entirely. Omitted means a producer record with no timestamp. */
  provenance?: Record<string, unknown> | null;
  /** Emit the `$schema` member. Default `true`, matching every published document. */
  schemaRef?: boolean;
  /**
   * The document's groups. Omitted means none, which is written as the empty array the current
   * contract requires — never as an absent member.
   */
  groups?: readonly ApmapGroup[];
  extensions?: Record<string, unknown>;
  /**
   * `Q2_002A`: told about every Classic face whose SER-7 spelling would have moved its texture by more than
   * `CLASSIC_FIDELITY_BOUND_TEXELS` — `rebased` (stored as other numbers that compile to the same texels) or
   * `unrepresentable` (kept literal; the texture moves by `errorTexels`, and the caller must say so).
   */
  onClassicFidelity?: (report: ClassicFidelityReport) => void;
};

/** One Classic face whose texture SER-7 would have moved, by its source address and its minted id. */
export type ClassicFidelityReport = ClassicFidelity & { path: string; face_id: string };

/** The producer identity written into `provenance`. Not a version of Auto-Pigeon. */
export const APMAP_ENCODER = { producer: '@auto-pigeon/quake-geometry', producer_version: '0.1.0' } as const;
/** The `$schema` a written document points at — the CURRENT contract's URL, derived like the version. */
const schemaUrl = () => `https://auto-pigeon.org/schemas/apmap/${currentApmapVersion()}/apmap.schema.json`;
const REGISTRY_NAME = /^[a-z][a-z0-9_]{0,63}$/;
const DOCUMENT_ID = /^[0-9A-Za-z][0-9A-Za-z._:-]{0,127}$/;
const MAP_STRING = /^[^"\r\n]*$/;

/**
 * The entity's properties in source order, interleaved with its brushes, as content slots (`Q2_002`, `Q2_002A`).
 *
 * `entries` is the source's order — repeats, and keys written after a brush. `properties` is the last-wins reading
 * a caller may have edited after the read. When the two no longer agree, the difference is applied to the ordered
 * list by the editor's own policy (`propertyEdits.ts`): a changed key moves its last occurrence, a removed key
 * loses every occurrence, a new key goes in before the first brush, and everything else keeps its value and its
 * place. Pure: neither list is touched. A model whose reader kept no order is `properties` in its own order.
 */
type Slot = { kind: 'property'; key: string; value: string } | { kind: 'brush'; index: number };
function orderedSlots(source: ParsedEntity): readonly Slot[] {
  const entries = source.entries ?? Object.entries(source.properties).map(([key, value]) => ({ key, value, afterBrushes: 0 }));
  const slots: Slot[] = [];
  let next = 0;
  for (let position = 0; position <= source.brushes.length; position += 1) {
    while (next < entries.length && entries[next].afterBrushes <= position) { slots.push({ kind: 'property', key: entries[next].key, value: entries[next].value }); next += 1; }
    if (position < source.brushes.length) slots.push({ kind: 'brush', index: position });
  }
  if (!source.entries) return slots;
  const edits = propertyEditsBetween(lastWinsProperties(source.entries), source.properties);
  return applyPropertyEdits<Slot>(slots, edits, (key, value) => ({ kind: 'property', key, value })).content;
}
const TEXTURE = /^[^\s"]+$/;

const vec3 = (point: readonly number[], path: string, member: string): ApmapVec3 =>
  [canonicalApmapNumber(point[0], path, member), canonicalApmapNumber(point[1], path, member), canonicalApmapNumber(point[2], path, member)];

/** A `.map` file name reduced to its stem, and to characters `document_id` allows. */
function documentIdFor(sourceMap: string | undefined, role: ApmapRole): string {
  if (!sourceMap) {
    throw new ApmapEncodeError('apmap_encode_document_id',
      'No document_id and no sourceMap: pass one of the two rather than have an identity invented for this document', null, 'SCH-2');
  }
  const stem = sourceMap.replace(/\.map$/i, '').replace(/[^0-9A-Za-z._-]+/g, '-').replace(/^[^0-9A-Za-z]+/, '');
  const id = `${stem || 'map'}:${role}`;
  return id.slice(0, 128);
}

function projection(face: ParsedFace, path: string): ApmapProjection {
  const { mapping } = face;
  const scale: [number, number] = [canonicalApmapNumber(mapping.scale.x, path, 'scale.x'), canonicalApmapNumber(mapping.scale.y, path, 'scale.y')];
  const rotation = canonicalApmapNumber(mapping.rotation, path, 'rotation');
  if (mapping.mode === 'valve220') {
    const axes = mapping.valve_220_axes;
    if (!axes) throw new ApmapEncodeError('apmap_encode_face_invalid', `${path}: a valve220 face carries no u/v axes`, path, 'SCH-8');
    return {
      mode: 'valve220',
      u_axis: [canonicalApmapNumber(axes.u.x, path, 'u_axis'), canonicalApmapNumber(axes.u.y, path, 'u_axis'),
               canonicalApmapNumber(axes.u.z, path, 'u_axis'), canonicalApmapNumber(axes.u.shift, path, 'u_axis')],
      v_axis: [canonicalApmapNumber(axes.v.x, path, 'v_axis'), canonicalApmapNumber(axes.v.y, path, 'v_axis'),
               canonicalApmapNumber(axes.v.z, path, 'v_axis'), canonicalApmapNumber(axes.v.shift, path, 'v_axis')],
      rotation, scale,
    };
  }
  return { mode: 'classic', shift: [canonicalApmapNumber(mapping.shift.x, path, 'shift.x'), canonicalApmapNumber(mapping.shift.y, path, 'shift.y')], rotation, scale };
}

/**
 * The Quake 2 numeric tail. The parsed model holds it as opaque strings because the `.map` lexer
 * does not interpret it; APMap types it as numbers, so a token that is not one is refused rather
 * than coerced to `NaN` and written into a document that then fails SEM-11 somewhere else.
 */
function tail(face: ParsedFace, path: string): number[] | undefined {
  if (!face.tail.length) return undefined;
  return face.tail.map((token) => {
    const value = Number(token);
    if (!Number.isFinite(value)) {
      throw new ApmapEncodeError('apmap_encode_tail_invalid', `${path}: trailing face value ${JSON.stringify(token)} is not a number; APMap 1.0 face tails are numeric`, path, 'SCH-14');
    }
    return canonicalApmapNumber(value, path, 'tail');
  });
}

export function apmapFromParsedMap(parsed: ParsedMap, options: ApmapEncodeOptions = {}): ApmapDocument {
  const role = options.role ?? 'full_map';
  const sourceMap = bareSourceMapName(options.sourceMap);
  const sourceSha256 = options.sourceSha256 ?? (options.sourceText === undefined ? undefined : sha256Hex(options.sourceText));
  const documentId = options.documentId ?? documentIdFor(sourceMap, role);
  if (!DOCUMENT_ID.test(documentId)) {
    throw new ApmapEncodeError('apmap_encode_document_id', `document_id ${JSON.stringify(documentId)} is not ${DOCUMENT_ID.source}`, null, 'SCH-2');
  }
  if (sourceSha256 !== undefined && !/^[0-9a-f]{64}$/.test(sourceSha256)) {
    throw new ApmapEncodeError('apmap_encode_source_sha256', 'sourceSha256 is not a lowercase hex SHA-256 digest', null, 'SCH-2');
  }
  const revision = options.revision ?? 0;
  if (!Number.isInteger(revision) || revision < 0) throw new ApmapEncodeError('apmap_encode_revision', `revision must be an integer ≥ 0, not ${revision}`, null, 'SCH-3');

  const valve = parsed.entities.some((entity) => entity.brushes.some((brush) => brush.faces.some((face) => face.mapping.mode === 'valve220')));
  const game = options.game ?? 'quake1';
  const mapDialect = options.mapDialect ?? (valve ? 'valve_220' : 'quake_standard');
  for (const [member, value] of [['game', game], ['map_dialect', mapDialect]] as const) {
    if (!REGISTRY_NAME.test(value)) throw new ApmapEncodeError('apmap_encode_registry_name', `${member} ${JSON.stringify(value)} is not a registry name`, null, 'SCH-2');
  }
  const derivedIds = (options.idPolicy ?? 'derived') === 'derived';

  /** Identity is by source address, so it survives a move, a re-export and a regeneration. */
  const identity = (kind: ApmapObjectKind, sourcePath: string): { id: string; derived_from?: ApmapDerivedFrom } => {
    const derived: ApmapDerivedFrom = sourceMapDerivedFrom(sourcePath, sourceMap, sourceSha256);
    const id = deriveApmapId(kind, derived);
    return derivedIds ? { id, derived_from: derived } : { id };
  };

  // `Q2_002A`: whose base-plane rule reads this document's Classic faces. Only `qbsp`'s is measured here; a Quake III
  // document is encoded by `quake3Encode.ts` and Q3Map2's rule is left exactly as it was.
  const faithfulClassic = classicReaderOf({ game }) === 'quake';

  const face = (source: ParsedFace, path: string, emittedCorners: (index: number) => number[][] | null, index: number,
    points: ApmapVec3[]): ApmapFace => {
    if (!TEXTURE.test(source.textureName)) {
      throw new ApmapEncodeError('apmap_encode_texture_invalid', `${path}: texture ${JSON.stringify(source.textureName)} contains whitespace or a quote`, path, 'SCH-14');
    }
    const { id, derived_from } = identity('face', path);
    const trailing = tail(source, path);
    let stored = projection(source, path);
    if (faithfulClassic && stored.mode === 'classic') {
      const { mapping } = source;
      const fidelity = faithfulClassicNumbers({
        sourcePoints: source.points, emittedPoints: points, reader: 'quake',
        source: { shift: [mapping.shift.x, mapping.shift.y], rotation: mapping.rotation, scale: [mapping.scale.x, mapping.scale.y] },
        corners: () => emittedCorners(index), round: (value) => canonicalApmapNumber(value, path, 'projection'),
      });
      if (fidelity.outcome === 'rebased') stored = { mode: 'classic', ...fidelity.numbers };
      if (fidelity.outcome === 'rebased' || fidelity.outcome === 'unrepresentable') options.onClassicFidelity?.({ ...fidelity, path, face_id: id });
    }
    // SER-5: face_id, derived_from, plane, texture, projection, tail — the specification's order.
    return { face_id: id, ...(derived_from ? { derived_from } : {}), plane: { points: points as ApmapFace['plane']['points'] },
             texture: source.textureName, projection: stored, ...(trailing ? { tail: trailing } : {}) };
  };

  const brush = (source: ParsedBrush, path: string): ApmapBrushItem => {
    if (source.faces.length < 4) {
      throw new ApmapEncodeError('apmap_encode_brush_degenerate', `${path}: a brush needs at least four faces, this one has ${source.faces.length}`, path, 'SCH-6');
    }
    const { id, derived_from } = identity('brush', path);
    const emitted = source.faces.map((item, index) => item.points.map((point) => vec3(point, `${path}.f${index}`, 'plane.points')));
    // The surface the DOCUMENT holds — its rounded points, clipped — built once, and only if a face needs it.
    let polygons: (number[][] | null)[] | undefined;
    const emittedCorners = (index: number) => {
      polygons ??= reconstructBrush({ ...source, faces: source.faces.map((item, f) => ({ ...item, points: emitted[f] as ParsedFace['points'] })) })
        .faces.map((item) => (item.status === 'renderable' ? item.polygon : null));
      return polygons[index];
    };
    return { kind: 'brush', brush_id: id, ...(derived_from ? { derived_from } : {}),
             faces: source.faces.map((item, index) => face(item, `${path}.f${index}`, emittedCorners, index, emitted[index])) };
  };

  const entity = (source: ParsedEntity, index: number): ApmapEntity => {
    const path = `e${index}`;
    const content: ApmapContentItem[] = [];
    // Every property string is checked before anything is returned: an entity that cannot be written is refused
    // whole, never written as far as its first bad key.
    for (const slot of orderedSlots(source)) {
      if (slot.kind === 'brush') { content.push(brush(source.brushes[slot.index], `${path}.b${slot.index}`)); continue; }
      if (!MAP_STRING.test(slot.key) || !MAP_STRING.test(slot.value)) {
        throw new ApmapEncodeError('apmap_encode_property_invalid', `${path}: property ${JSON.stringify(slot.key)} contains a quote or a line break, which no MAP writer can emit`, path, 'SCH-5');
      }
      content.push({ kind: 'property', key: slot.key, value: slot.value });
    }
    if (!content.length) throw new ApmapEncodeError('apmap_encode_entity_empty', `${path}: an entity with no properties and no brushes cannot be represented`, path, 'SCH-12');
    const { id, derived_from } = identity('entity', path);
    return { entity_id: id, ...(derived_from ? { derived_from } : {}), content };
  };

  const provenance = options.provenance === null ? undefined : options.provenance ?? {
    ...APMAP_ENCODER,
    pipeline: ['map-import'],
    ...(sourceMap || sourceSha256 ? { source: { ...(sourceMap ? { map_name: sourceMap } : {}), ...(sourceSha256 ? { map_sha256: sourceSha256 } : {}) } } : {}),
  };

  // SER-5 again, at document level. The entities are built last but inserted where they belong.
  const entities = parsed.entities.map((item, index) => entity(item, index));
  return {
    ...(options.schemaRef === false ? {} : { $schema: schemaUrl() }),
    // WRITE current only. Derived, never named: the version comes from the contract the host
    // configured, so a producer cannot emit a format the validator does not speak.
    apmap_version: currentApmapVersion(),
    document_id: documentId,
    revision,
    role,
    game,
    map_dialect: mapDialect,
    id_policy: derivedIds ? 'derived' : 'minted',
    frame: options.frame ?? { space: 'source_world', units: 'quake_unit', coordinate_system: { handedness: 'right', up_axis: 'z' }, to_source: { type: 'identity' } },
    ...(provenance ? { provenance } : {}),
    entities,
    // Always emitted, empty or not. `groups` is required of a current writer, and an absent one
    // would be indistinguishable from a producer that has never heard of groups — which is exactly
    // the ambiguity a tool that drops what it does not understand would exploit.
    groups: options.groups ? options.groups.map((group) => ({ ...group, members: [...group.members] })) : [],
    ...(options.extensions ? { extensions: options.extensions } : {}),
  } as ApmapDocument;
}

/**
 * The canonical bytes of a document.
 *
 * Kept as the name every consumer already calls, but it is no longer `JSON.stringify` of whatever
 * order the object happens to be in: it delegates to `canonicalApmapText`, which *imposes* SER-5
 * and SER-7 rather than assuming the caller's value already satisfies them. For this encoder's own
 * output that is a no-op, which is the point — the encoder stays canonical by construction and a
 * document from anywhere else gets the same bytes anyway.
 */
export const apmapDocumentText = (document: ApmapDocument): string => canonicalApmapText(document);
