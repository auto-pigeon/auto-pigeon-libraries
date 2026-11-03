/**
 * The canonical serialization of an APMap document — one document, one byte sequence.
 *
 * ### Why this exists as its own module
 *
 * `encode.ts` was already "canonical by construction": it inserts every member in the
 * specification's declared order and puts every number through SER-7, so `JSON.stringify` of *its*
 * output was already canonical, and `apmapDocumentText` was that one line. That property belongs to
 * the encoder's output and to nothing else. A document that came off disk, over a socket, or out of
 * an edit carries whatever member order its producer used, and `JSON.stringify` preserves insertion
 * order — so the same document written by two processes that built it differently produced two
 * different byte sequences.
 *
 * This module is the missing half: it **imposes** the canonical form rather than assuming it. The
 * property it exists to give is
 *
 *     write(read(write(read(x))))  ===  write(read(x))
 *
 * byte for byte, for any document, however its members happen to be ordered in memory — which is
 * what a collaborative editor's "every client converges on the same bytes" eventually stands on.
 * `apmapDocumentText` now delegates here, so there is one writer rather than two that must agree.
 *
 * ### The rules, in full
 *
 * 1. **Member order is the schema's declared order** (SER-5), per object kind, from the tables
 *    below. A member the schema does not declare is not dropped: it is written *after* the declared
 *    ones, sorted by code unit. Losing data to make bytes tidy would be the wrong trade, and
 *    sorting is what makes the leftover deterministic.
 * 2. **Open objects sort.** `extensions`, `payload`, `parameters` and `metadata` are objects the
 *    schema deliberately does not describe, so there is no declared order to follow; their keys are
 *    sorted by code unit, recursively.
 * 3. **Arrays never reorder.** Entity `content` order is semantic — a repeated key and a key's
 *    position relative to a brush both mean something — and so are face order, plane points and the
 *    Quake 2 numeric tail. Nothing here sorts an array.
 * 4. **Numbers are SER-7**: finite, at most six decimal places, and `-0` written as `0`. See
 *    `canonicalApmapNumber`. Integers keep integer spelling (`192`, never `192.0`); a rounded value
 *    keeps the shortest spelling that reproduces it (`0.5`, never `0.500000`), which is what
 *    `JSON.stringify` produces for a `number` and is identical across engines.
 * 5. **Two-space indent, `\n` line endings, one trailing newline** (SER-1 … SER-4), from
 *    `JSON.stringify(value, null, 2)`. String escaping is `JSON.stringify`'s, which ECMA-262
 *    specifies exactly (QuoteJSONString) — it is not an implementation detail that can drift.
 * 6. **No wall clock.** Nothing here adds `provenance.generated_at`; a document that carries one
 *    round-trips it unchanged and forfeits reproducibility itself (SER-8).
 *
 * ### What this is not
 *
 * It is not validation. A document with a member the schema forbids is written, in canonical form,
 * with that member still on it — `validate.ts` is what answers whether a document is well-formed,
 * when a caller asks. The one thing that throws is a non-finite number, because JSON has no
 * spelling for it at all and the alternative is emitting `null` where a coordinate belongs.
 */
import type { ApmapDocument } from './types.ts';

export class ApmapEncodeError extends Error {
  constructor(readonly code: string, message: string, readonly path: string | null = null, readonly rule = '') {
    super(message);
    this.name = 'ApmapEncodeError';
  }
}

/**
 * SER-7: finite, six decimal places, no `-0`.
 *
 * Six is the format's own figure, not a choice made here. `-0` matters because it is a distinct
 * IEEE value that `JSON.stringify` writes as `0` anyway — collapsing it at the value level keeps
 * the in-memory document and the bytes in agreement rather than having the writer quietly differ.
 */
export function canonicalApmapNumber(value: number, path: string, member: string): number {
  if (!Number.isFinite(value)) throw new ApmapEncodeError('apmap_encode_non_finite', `${path}: ${member} is ${value}; APMap forbids NaN and Infinity`, path, 'SEM-11');
  const rounded = Math.round(value * 1e6) / 1e6;
  return Object.is(rounded, -0) ? 0 : rounded;
}

/**
 * The declared member order for every object kind APMap describes.
 *
 * Transcribed from the current `auto-pigeon-libraries/ts/apmap-schema/schema/apmap-*.schema.json` —
 * the authority on the shape — in the order that file declares them. The key of each entry is the
 * *discriminator* an object is recognised by, resolved in `orderFor`; there is no reflection here
 * and no attempt to read the schema at runtime, because this package loads the schema rather than
 * embedding it and must still be able to write when no schema was supplied.
 */
const ORDER = {
  document: ['$schema', 'apmap_version', 'document_id', 'revision', 'role', 'game', 'map_dialect', 'id_policy', 'frame', 'provenance', 'authorship', 'entities', 'groups', 'relationships', 'extensions'],
  authorship: ['author', 'copyright_notice', 'license'],
  frame: ['space', 'units', 'coordinate_system', 'to_source'],
  coordinate_system: ['handedness', 'up_axis'],
  transform: ['type', 'translation'],
  provenance: ['producer', 'producer_version', 'pipeline', 'source', 'package', 'generated_at', 'notes'],
  provenance_source: ['map_name', 'map_sha256', 'corpus'],
  provenance_package: ['package_id', 'role_file', 'revision'],
  entity: ['entity_id', 'derived_from', 'content', 'extensions'],
  group: ['group_id', 'name', 'members', 'source'],
  // One arm per kind, flattened: `declared.filter((key) => key in value)` keeps only the id field
  // the member actually carries, and the discriminator leads, as every arm declares it.
  group_member: ['kind', 'entity_id', 'brush_id', 'face_id', 'group_id', 'patch_id'],
  group_source: ['kind', 'prefab_id'],
  property_item: ['kind', 'key', 'value'],
  brush_item: ['kind', 'brush_id', 'derived_from', 'faces', 'broken', 'extensions'],
  // APMap 1.5 (`Q3_001`): a Quake III patch and a brushDef face's projection. Their numbers are SER-7Q.
  patch_item: ['kind', 'patch_id', 'derived_from', 'texture', 'width', 'height', 'control_points', 'tail', 'extensions'],
  face: ['face_id', 'derived_from', 'plane', 'texture', 'projection', 'tail', 'synthetic', 'synthetic_reason', 'extensions'],
  plane: ['points'],
  projection_classic: ['mode', 'shift', 'rotation', 'scale'],
  projection_valve220: ['mode', 'u_axis', 'v_axis', 'rotation', 'scale'],
  projection_brush_primitives: ['mode', 'matrix'],
  bounds: ['min', 'max'],
  relationship: ['relationship_id', 'type', 'from', 'to', 'payload'],
  derived_from_source_map: ['kind', 'source_map', 'source_sha256', 'source_path', 'source_entity_index', 'source_brush_position', 'origin_at_extraction', 'bounds_at_extraction'],
  derived_from_package: ['kind', 'package_id', 'role', 'object_path', 'source_map', 'source_entity_index', 'source_brush_position', 'origin_at_extraction', 'bounds_at_extraction'],
  derived_from_authored: ['kind', 'document_id', 'operation_id', 'ordinal'],
  derived_from_synthetic: ['kind', 'generator', 'reason', 'source_object', 'parameters'],
  derived_from_operation: ['kind', 'operation_id', 'operation', 'ordinal', 'replaces', 'intent', 'metadata'],
} as const;

/** The members whose contents the schema deliberately leaves open, and which therefore sort. */
const OPEN = new Set(['extensions', 'payload', 'parameters', 'metadata']);

/**
 * `exact` is the SER-7Q slot: the numbers of a patch control point and of a brush-primitives matrix.
 * They are written as the shortest decimal that reproduces the double they are — never rounded to six
 * places, which would change a `0.00260417` texture coordinate or a `0.0078125` (1/128) coefficient.
 */
type Slot = keyof typeof ORDER | 'open' | 'unknown' | 'exact';

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

/** Which `derived_from` variant this is. An unrecognised `kind` sorts rather than throwing. */
function derivedFromSlot(value: Record<string, unknown>): Slot {
  switch (value.kind) {
    case 'source_map': return 'derived_from_source_map';
    case 'package': return 'derived_from_package';
    case 'authored': return 'derived_from_authored';
    case 'synthetic': return 'derived_from_synthetic';
    case 'operation': return 'derived_from_operation';
    default: return 'unknown';
  }
}

/**
 * Serialize one value. `slot` says what kind of object the caller believes this is; the value's own
 * discriminator refines it where there is one (`kind`, `mode`, `type`).
 *
 * Everything is rebuilt rather than mutated: the document a caller passed in is not touched, which
 * matters because the obvious caller holds it as live editor state.
 */
function canonical(value: unknown, slot: Slot, path: string): unknown {
  if (typeof value === 'number') return slot === 'exact' ? exactApmapNumber(value, path) : canonicalApmapNumber(value, path, 'value');
  if (Array.isArray(value)) return value.map((item, index) => canonical(item, itemSlot(slot), `${path}/${index}`));
  if (!isObject(value)) return value;

  const resolved = refine(slot, value);
  const declared = resolved === 'open' || resolved === 'unknown' || resolved === 'exact' ? [] : (ORDER[resolved] as readonly string[]);
  const rest = Object.keys(value).filter((key) => !declared.includes(key)).sort();
  const output: Record<string, unknown> = {};
  for (const key of [...declared.filter((key) => key in value), ...rest]) {
    output[key] = canonical(value[key], memberSlot(resolved, key), `${path}/${key}`);
  }
  return output;
}

/** The slot for the items of an array whose own slot is `slot`. */
function itemSlot(slot: Slot): Slot {
  switch (slot) {
    case 'entity': return 'entity';
    case 'face': return 'face';
    case 'relationship': return 'relationship';
    case 'property_item': return 'property_item';
    // `groups` resolves to `group`, so its ITEMS are groups. Until 1.3 this arm did not exist and
    // `ORDER.group` was unreachable: every group was written with its members sorted
    // alphabetically — `group_id, members, name` — rather than in the order SER-5 declares. It went
    // unnoticed because the three names happen to round-trip either way. `source` is what made it
    // visible: a fourth member sorts after `name` and before nothing, which is not where the
    // contract puts it.
    case 'group': return 'group';
    case 'group_member': return 'group_member';
    case 'exact': return 'exact';
    default: return slot === 'open' ? 'open' : 'unknown';
  }
}

/** Refine a slot by the value's own discriminator, for the three unions the format has. */
function refine(slot: Slot, value: Record<string, unknown>): Slot {
  if (slot === 'property_item') return value.kind === 'brush' ? 'brush_item' : value.kind === 'patch' ? 'patch_item' : 'property_item';
  if (slot === 'derived_from_source_map') return derivedFromSlot(value);
  if (slot === 'projection_classic')
    return value.mode === 'valve220' ? 'projection_valve220' : value.mode === 'brush_primitives' ? 'projection_brush_primitives' : 'projection_classic';
  return slot;
}

/** The slot a member of an object in `slot` belongs to. Anything unlisted is `unknown` — kept, sorted. */
function memberSlot(slot: Slot, key: string): Slot {
  if (slot === 'open' || OPEN.has(key)) return 'open';
  if (key === 'derived_from') return 'derived_from_source_map';   // refined by `kind`
  switch (slot) {
    case 'document':
      return key === 'frame' ? 'frame' : key === 'provenance' ? 'provenance'
        : key === 'authorship' ? 'authorship'
        : key === 'entities' ? 'entity' : key === 'groups' ? 'group'
        : key === 'relationships' ? 'relationship' : 'unknown';
    case 'group': return key === 'members' ? 'group_member' : key === 'source' ? 'group_source' : 'unknown';
    case 'frame': return key === 'coordinate_system' ? 'coordinate_system' : key === 'to_source' ? 'transform' : 'unknown';
    case 'provenance': return key === 'source' ? 'provenance_source' : key === 'package' ? 'provenance_package' : 'unknown';
    // `content` is the union; `property_item` is the slot that gets refined to `brush_item` by kind.
    case 'entity': return key === 'content' ? 'property_item' : 'unknown';
    case 'brush_item': return key === 'faces' ? 'face' : 'unknown';
    case 'patch_item': return key === 'control_points' ? 'exact' : 'unknown';
    case 'projection_brush_primitives': return key === 'matrix' ? 'exact' : 'unknown';
    case 'face': return key === 'plane' ? 'plane' : key === 'projection' ? 'projection_classic' : 'unknown';
    case 'derived_from_source_map':
    case 'derived_from_package':
      return key === 'bounds_at_extraction' ? 'bounds' : 'unknown';
    default: return 'unknown';
  }
}

/**
 * The document, rebuilt in canonical member order with every number through SER-7.
 *
 * Useful on its own when a caller wants to compare or hash structures rather than text; the bytes
 * come from `canonicalApmapText`.
 */
export const canonicalApmapDocument = (document: ApmapDocument): ApmapDocument =>
  canonical(document, 'document', '') as ApmapDocument;

/**
 * The canonical bytes of a document: SER-1 … SER-7 and SER-7Q, imposed rather than assumed.
 *
 * `JSON.stringify` already writes the shortest round-trip spelling of a number, which is SER-7Q —
 * except that it switches to exponent notation below 1e-6, and SER-7Q is positional. A SER-7 number
 * never gets there (six places stop at 0.000001), so only an `exact` value can, and only as an array
 * element on a line of its own; `positionalNumbers` rewrites exactly those lines.
 */
export const canonicalApmapText = (document: ApmapDocument): string =>
  `${positionalNumbers(JSON.stringify(canonicalApmapDocument(document), null, 2))}\n`;

/**
 * SER-7Q: finite, `-0` as `0`, and otherwise the value exactly as it is — no rounding.
 *
 * Bounded by the schema (±131072 for a control point, ±1e6 for a matrix coefficient), so the
 * shortest spelling never reaches `JSON.stringify`'s large-number exponent form.
 */
export function exactApmapNumber(value: number, path: string): number {
  if (!Number.isFinite(value)) throw new ApmapEncodeError('apmap_encode_non_finite', `${path}: value is ${value}; APMap forbids NaN and Infinity`, path, 'SEM-11');
  return Object.is(value, -0) ? 0 : value;
}

/** An array element line that `JSON.stringify` wrote in exponent notation. Strings are quoted, so never match. */
const EXPONENT_LINE = /^(\s*)(-?)(\d)(?:\.(\d+))?e([+-]\d+)(,?)$/gm;

/** Rewrite each exponent-notation number to the same digits in positional notation — string work, no arithmetic. */
function positionalNumbers(text: string): string {
  return text.replace(EXPONENT_LINE, (_line, indent: string, sign: string, lead: string, rest: string | undefined, exponent: string, comma: string) => {
    const digits = lead + (rest ?? '');
    const point = 1 + Number(exponent);      // where the decimal point falls, counted from the first digit
    const positional = point <= 0 ? `0.${'0'.repeat(-point)}${digits}`
      : point >= digits.length ? digits + '0'.repeat(point - digits.length)
      : `${digits.slice(0, point)}.${digits.slice(point)}`;
    return `${indent}${sign}${positional}${comma}`;
  });
}
