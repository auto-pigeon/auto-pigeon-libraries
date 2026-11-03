/**
 * APMap → `.map` text. The export half of the codec.
 *
 * The document is written straight out, not routed through `ParsedMap` first: an entity's `content`
 * is an **ordered** list in which a repeated key and the position of a key relative to a brush are
 * both significant, and the parsed model's property map cannot hold either. Reading the document
 * directly is what makes `.map → APMap → .map` preserve what the format promises to preserve.
 *
 * What is dropped is what `.map` has nowhere to put (`@auto-pigeon/apmap-schema`'s `SEMANTICS.md`;
 * §14 of the archived 1.0 specification):
 * `document_id`, every object id, `derived_from`, `relationships` and `extensions`. **Re-importing
 * an exported `.map` without its APMap sibling therefore mints a new document with new ids** — that
 * is the whole reason APMap is the canonical form and `.map` is an interchange representation.
 *
 * The frame is flattened too. A `prefab_local` document writes local coordinates unless the caller
 * asks for `frame: 'source'`, which applies `frame.to_source` and writes source-world ones.
 */
import { isApmapBrush, type ApmapBrushItem, type ApmapDocument, type ApmapEntity, type ApmapFace, type ApmapVec3 } from '../apmap/types.ts';
import type { ApmapBrushPrimitivesProjection, ApmapPatchItem } from '../apmap/types.ts';
import { QUAKE3_GAME_NAME, quake3MapShaderName } from '../apmap/quake3Names.ts';
import { transportBrushPrimitives } from '../geometry/brushPrimitives.ts';
import { checkPatch } from '../geometry/quake3PatchCheck.ts';

export class MapExportError extends Error {
  constructor(readonly code: string, message: string, readonly path: string | null = null) {
    super(message);
    this.name = 'MapExportError';
  }
}

export type MapExportOptions = {
  /**
   * `document` (the default) writes the coordinates the document holds. `source` applies
   * `frame.to_source` first, so a prefab lands where it was extracted from.
   */
  frame?: 'document' | 'source';
  /** `// entity 0` / `// brush 0` markers. On by default; the lexer discards them on the way back. */
  comments?: boolean;
  /** Indent inside an entity. Two spaces by default; `''` matches id Software's own files. */
  indent?: string;
};

/**
 * A `.map` number. The lexer's grammar has no exponent, so `1e-7` must never be written: a value
 * too small to spell decimally is written as its six-decimal rounding, which is what SER-7 already
 * made of it at encode time. Integers stay integers — `192`, never `192.0`.
 */
export function formatMapNumber(value: number, path: string): string {
  if (!Number.isFinite(value)) throw new MapExportError('map_export_non_finite', `${path}: cannot write ${value} to a .map`, path);
  if (Number.isInteger(value) && Math.abs(value) < 1e21) return String(value === 0 ? 0 : value);
  const plain = String(value);
  if (!/[eE]/.test(plain)) return plain;
  const fixed = value.toFixed(6).replace(/\.?0+$/, '');
  return fixed === '-0' || fixed === '' ? '0' : fixed;
}

/**
 * A number that must survive the trip EXACTLY — a brushDef matrix coefficient (SER-7Q). The shortest
 * round-trip spelling, rewritten positionally when JavaScript would use an exponent, so the digits
 * Q3Map2 reads are the digits the document holds. `formatMapNumber`'s six-decimal fallback would
 * turn `1/32768` into `0.000031`.
 */
export function formatExactMapNumber(value: number, path: string): string {
  if (!Number.isFinite(value)) throw new MapExportError('map_export_non_finite', `${path}: cannot write ${value} to a .map`, path);
  if (Object.is(value, -0) || value === 0) return '0';
  const plain = String(value);
  const match = /^(-?)(\d)(?:\.(\d+))?e([+-]\d+)$/.exec(plain);
  if (!match) return plain;
  const [, sign, lead, rest = '', exponent] = match;
  const digits = lead + rest, point = 1 + Number(exponent);
  const positional = point <= 0 ? `0.${'0'.repeat(-point)}${digits}`
    : point >= digits.length ? digits + '0'.repeat(point - digits.length) : `${digits.slice(0, point)}.${digits.slice(point)}`;
  return sign + positional;
}

const point = (value: ApmapVec3, offset: ApmapVec3, path: string): string =>
  `( ${value.map((component, axis) => formatMapNumber(component + offset[axis], path)).join(' ')} )`;

function faceLine(face: ApmapFace, offset: ApmapVec3, path: string, quake3 = false): string {
  const points = face.plane?.points;
  if (!Array.isArray(points) || points.length !== 3) throw new MapExportError('map_export_face_invalid', `${path}: a face plane needs exactly three points`, path);
  if (typeof face.texture !== 'string' || !/^[^\s"]+$/.test(face.texture)) {
    throw new MapExportError('map_export_texture_invalid', `${path}: texture ${JSON.stringify(face.texture)} cannot be written to a .map`, path);
  }
  const projection = face.projection;
  // Quake III: Q3Map2 prefixes `textures/` itself, so a name that already carries it would compile as
  // `textures/textures/…` (measured). Every other game writes the name exactly as stored.
  const texture = quake3 ? quake3MapShaderName(face.texture) : face.texture;
  const plane = points.map((item) => point(item as ApmapVec3, offset, path)).join(' ');
  const head = `${plane} ${texture}`;
  const number = (value: number) => formatMapNumber(value, path);
  const tail = face.tail?.length ? ` ${face.tail.map(number).join(' ')}` : '';
  if (projection?.mode === 'brush_primitives') {
    if (!quake3) throw new MapExportError('map_export_projection_invalid', `${path}: a brushDef texture matrix is Quake III syntax and this is not a Quake III document`, path);
    // After the translation offset: the matrix is written against the plane points as WRITTEN, and a
    // `frame: 'source'` export that moved them must move the texture with them.
    const matrix = offset.some((value) => value !== 0) ? translatedMatrix(face, offset, path) : projection.matrix;
    const row = (values: readonly number[]) => `( ${values.map((value) => formatExactMapNumber(value, path)).join(' ')} )`;
    return `${plane} ( ${row(matrix[0])} ${row(matrix[1])} ) ${texture}${tail}`;
  }
  if (projection?.mode === 'valve220') {
    const axis = (values: readonly number[]) => `[ ${values.map(number).join(' ')} ]`;
    return `${head} ${axis(projection.u_axis)} ${axis(projection.v_axis)} ${number(projection.rotation)} ${projection.scale.map(number).join(' ')}${tail}`;
  }
  if (projection?.mode !== 'classic') throw new MapExportError('map_export_projection_invalid', `${path}: projection.mode must be 'classic' or 'valve220'`, path);
  return `${head} ${projection.shift.map(number).join(' ')} ${number(projection.rotation)} ${projection.scale.map(number).join(' ')}${tail}`;
}

/**
 * One patch as Q3Map2's `patchDef2` (`Q3_003`):
 *
 * ```text
 * {
 *   patchDef2
 *   {
 *     base_wall/metal
 *     ( 3 3 0 0 0 )
 *     (
 *       ( ( x y z s t ) ( x y z s t ) ( x y z s t ) )     <- column 0, rows 0…height−1
 *       …
 *     )
 *   }
 * }
 * ```
 *
 * One parenthesised group per COLUMN, `height` points in each — `ParsePatch`'s own loop order, and
 * APMap's `control_points[c][r]` (SEMANTICS §9c). Every number is written exactly (SER-7Q), because a
 * control point's `s, t` are texture repeats and six decimals would move a 1/384 coordinate. The
 * header's three trailing numbers are the patch's `tail`, `0 0 0` when it has none.
 */
function patchBlock(patch: ApmapPatchItem, offset: ApmapVec3, path: string, indent: string, lines: string[]): void {
  // Structural only: a collapsed net that was already in the map is written as it is (Q3Map2 builds it).
  const check = checkPatch(patch, { structural: true });
  if (!check.ok) throw new MapExportError('map_export_patch_invalid', `${path}: Quake III patch ${patch.patch_id} cannot be written (${check.fault}: ${check.detail}); nothing was exported`, path);
  const exact = (value: number) => formatExactMapNumber(value, path);
  const tail = Array.isArray(patch.tail) && patch.tail.length === 3 ? patch.tail : [0, 0, 0];
  const i1 = indent, i2 = indent + indent, i3 = i2 + indent, i4 = i3 + indent;
  lines.push(`${i1}{`, `${i2}patchDef2`, `${i2}{`, `${i3}${quake3MapShaderName(patch.texture)}`,
             `${i3}( ${patch.width} ${patch.height} ${tail.map(exact).join(' ')} )`, `${i3}(`);
  for (const column of patch.control_points)
    lines.push(`${i4}( ${column.map(([x, y, z, s, t]) => `( ${[x + offset[0], y + offset[1], z + offset[2]].map(exact).join(' ')} ${exact(s)} ${exact(t)} )`).join(' ')} )`);
  lines.push(`${i3})`, `${i2}}`, `${i1}}`);
}

/** A brushDef matrix carried through the export's translation offset — exact, like any rigid move. */
function translatedMatrix(face: ApmapFace, offset: ApmapVec3, path: string) {
  const moved = transportBrushPrimitives(face.projection as ApmapBrushPrimitivesProjection, face.plane.points,
                                         { linear: [[1, 0, 0], [0, 1, 0], [0, 0, 1]], translation: [offset[0], offset[1], offset[2]] }, (value) => value);
  if ('refusal' in moved) throw new MapExportError('map_export_face_invalid', `${path}: the brushDef texture matrix could not be placed (${moved.refusal})`, path);
  return moved.projection.matrix;
}

function entityBlock(entity: ApmapEntity, index: number, offset: ApmapVec3, options: Required<MapExportOptions>, lines: string[], quake3 = false): void {
  const path = `/entities/${index}`;
  if (!Array.isArray(entity?.content)) throw new MapExportError('map_export_entity_invalid', `${path}: content must be an array`, path);
  if (options.comments) lines.push(`// entity ${index}`);
  lines.push('{');
  let brushes = 0, patches = 0;
  for (const [position, item] of entity.content.entries()) {
    const itemPath = `${path}/content/${position}`;
    if (isApmapBrush(item)) {
      if (!Array.isArray(item.faces) || item.faces.length < 4) throw new MapExportError('map_export_brush_invalid', `${itemPath}: a brush needs at least four faces`, itemPath);
      if (options.comments) lines.push(`${options.indent}// brush ${brushes}`);
      lines.push(`${options.indent}{`);
      // Quake III's second brush syntax: `{ brushDef { faces } }`. A brush is one or the other (SEM-Q-5).
      const primitives = quake3 && item.faces.some((face) => face.projection?.mode === 'brush_primitives');
      const faceIndent = primitives ? `${options.indent}${options.indent}${options.indent}` : `${options.indent}${options.indent}`;
      if (primitives) lines.push(`${options.indent}${options.indent}brushDef`, `${options.indent}${options.indent}{`);
      for (const [face, source] of item.faces.entries()) lines.push(`${faceIndent}${faceLine(source, offset, `${itemPath}/faces/${face}`, quake3)}`);
      if (primitives) lines.push(`${options.indent}${options.indent}}`);
      lines.push(`${options.indent}}`);
      brushes += 1;
      continue;
    }
    // APMap 1.5: a Quake III patch is written as Q3Map2's `patchDef2` (`Q3_003`), in its place among
    // the entity's items. Only in a Quake III document — SEM-Q-4 — and only a patch that passes the
    // same check an edit meets, so a file Q3Map2 would refuse (`ParsePatch: bad size`) is never written.
    if (item?.kind === 'patch') {
      if (!quake3) throw new MapExportError('map_export_patch_game', `${itemPath}: Quake III patch ${item.patch_id} is Quake III syntax and this is not a Quake III document; nothing was exported`, itemPath);
      if (options.comments) lines.push(`${options.indent}// patch ${patches}`);
      patchBlock(item, offset, itemPath, options.indent, lines);
      patches += 1;
      continue;
    }
    if (item?.kind !== 'property') throw new MapExportError('map_export_content_invalid', `${itemPath}: content item kind must be 'property' or 'brush'`, itemPath);
    // `"` and a line break end the token; a MAP writer has no escape for either (schema `map_string`).
    for (const [member, value] of [['key', item.key], ['value', item.value]] as const) {
      if (typeof value !== 'string' || /["\r\n]/.test(value)) throw new MapExportError('map_export_property_invalid', `${itemPath}: ${member} ${JSON.stringify(value)} cannot be written to a .map`, itemPath);
    }
    lines.push(`${options.indent}"${item.key}" "${item.value}"`);
  }
  lines.push('}');
}

/**
 * What a Quake III document must satisfy before a byte is written — `Q3_002`.
 *
 * **One brush syntax per file.** Q3Map2 fixes classic, Valve 220 or brushDef from the first brush and
 * refuses a later brush in another (measured, `Q3_001` and `Q3_002`: `MatchToken` failures). A mixed document is
 * refused whole, with every count — never exported as a file the compiler will reject, and never
 * converted here: a writer that rewrote brushes would export something other than the document.
 * Classic and Valve 220 are rewritten into each other where content ENTERS a map (`geometry/brushSyntax.ts`,
 * `Q3_009A`), so a document only comes to mix them by holding brushDef beside another, or by predating that.
 *
 * A brush that mixes the two inside itself is SEM-Q-5 and is refused the same way.
 */
function quake3ExportPreflight(document: ApmapDocument): void {
  const counts = { classic: 0, valve220: 0, brush_primitives: 0 };
  document.entities.forEach((entity, entityIndex) => {
    (Array.isArray(entity?.content) ? entity.content : []).forEach((item, position) => {
      if (!isApmapBrush(item) || !Array.isArray(item.faces)) return;
      const modes = new Set((item as ApmapBrushItem).faces.map((face) => (face?.projection?.mode === 'valve220' || face?.projection?.mode === 'brush_primitives') ? face.projection.mode : 'classic'));
      if (modes.size > 1)
        throw new MapExportError('map_export_quake3_mixed_brush', `/entities/${entityIndex}/content/${position}: brush ${item.brush_id} mixes face syntaxes (${[...modes].join(', ')}); nothing was exported`,
                                 `/entities/${entityIndex}/content/${position}`);
      counts[[...modes][0] as keyof typeof counts] += 1;
    });
  });
  const used = Object.entries(counts).filter(([, count]) => count > 0);
  if (used.length > 1)
    throw new MapExportError('map_export_quake3_mixed_syntax',
      `This Quake III map mixes brush syntaxes (${used.map(([syntax, count]) => `${count} ${syntax === 'brush_primitives' ? 'brushDef' : syntax === 'valve220' ? 'Valve 220' : 'classic'}`).join(', ')}). `
      + 'Q3Map2 reads one brush syntax per file; nothing was exported.');
}

const QUAKE2_GAME_NAME = 'quake2';
const INT32_MIN = -2147483648;
const INT32_MAX = 2147483647;
/** Entity keys a Quake II build resolves to a file under the game root. `*1` is an inline model. */
const QUAKE2_RESOURCE_KEYS = new Set(['model', 'noise', 'sky']);
const leavesGameRoot = (name: string): boolean =>
  /^[\\/]/.test(name) || /^[A-Za-z]:/.test(name)
  || name.replaceAll('\\', '/').split('/').some((segment) => segment === '' || segment === '.' || segment === '..');

/**
 * What a Quake II document must satisfy before a byte is written — the AULIBS contract's ADM-Q2-1..4
 * (`SEMANTICS.md` §6.3a, `Q2_001`). Not validity rules: APMap 1.5's face `tail` is any number of
 * finite numbers, so a document that trips one is a valid document every reader keeps. It is not a
 * `.map` a Quake II compiler reads as written — measured on the pinned EricW builds, four numbers
 * stop the parse, `1.5` is truncated by `std::stoi` with no diagnostic, and `2147483648` crashes one
 * build and wraps on the other — so the export is refused whole, naming the face, and the tail is
 * never rewritten. An ABSENT tail is not a fault: it means "use the WAL header".
 */
function quake2ExportPreflight(document: ApmapDocument): void {
  document.entities.forEach((entity, entityIndex) => {
    (Array.isArray(entity?.content) ? entity.content : []).forEach((item, position) => {
      const where = `/entities/${entityIndex}/content/${position}`;
      const property = item as { kind?: string; key?: string; value?: string };
      if (property.kind === 'property' && QUAKE2_RESOURCE_KEYS.has(property.key ?? '') && property.value
          && !property.value.startsWith('*') && leavesGameRoot(property.value))
        throw new MapExportError('map_export_quake2_reference_unsafe',
          `${where}: ${entity.entity_id} key ${property.key} "${property.value}" would resolve outside the game root (ADM-Q2-4); nothing was exported`, where);
      if (!isApmapBrush(item) || !Array.isArray(item.faces)) return;
      (item as ApmapBrushItem).faces.forEach((face, faceIndex) => {
        const at = `${where}/faces/${faceIndex}`;
        const refuse = (code: string, rule: string, message: string): never => {
          throw new MapExportError(code, `${at}: face ${face.face_id} ${message} (${rule}); nothing was exported`, at);
        };
        if (typeof face.texture === 'string' && leavesGameRoot(face.texture))
          refuse('map_export_quake2_reference_unsafe', 'ADM-Q2-4', `texture "${face.texture}" would resolve outside the game root`);
        if (face.tail === undefined || face.tail === null) return;
        const tail = (face.tail as unknown[]).map((value) => (typeof value === 'number' ? value : Number(value)));
        if (tail.length !== 3)
          refuse('map_export_quake2_tail_arity', 'ADM-Q2-1', `has a ${tail.length}-number tail; a Quake II tail is CONTENTS SURFACE_FLAGS VALUE`);
        tail.forEach((value, index) => {
          if (!Number.isInteger(value))
            refuse('map_export_quake2_tail_not_integer', 'ADM-Q2-2', `tail[${index}] = ${value} is not an integer`);
          if (value < INT32_MIN || value > INT32_MAX) {
            const hint = value > INT32_MAX && value <= 4294967295 ? ` (its signed 32-bit spelling is ${value | 0})` : '';
            refuse('map_export_quake2_tail_out_of_int32', 'ADM-Q2-3', `tail[${index}] = ${value} does not fit a signed 32-bit int${hint}`);
          }
        });
      });
    });
  });
}

/** Quake `.map` text for a document: LF endings, one trailing newline, source order throughout. */
export function mapTextFromApmap(document: ApmapDocument, options: MapExportOptions = {}): string {
  const resolved: Required<MapExportOptions> = { frame: options.frame ?? 'document', comments: options.comments ?? true, indent: options.indent ?? '  ' };
  if (!document || !Array.isArray(document.entities)) throw new MapExportError('map_export_malformed', 'APMap document has no entities array');
  let offset: ApmapVec3 = [0, 0, 0];
  if (resolved.frame === 'source') {
    // Widened deliberately: a document handed to the writer has not necessarily been validated, so
    // `to_source` may be a shape the 1.0 types do not admit, and that must be reported, not crash.
    const transform = document.frame?.to_source as { type?: string; translation?: unknown } | undefined;
    if (transform?.type === 'translation') {
      const translation = transform.translation;
      if (!Array.isArray(translation) || translation.length !== 3 || !translation.every((value) => Number.isFinite(value))) {
        throw new MapExportError('map_export_frame_invalid', 'frame.to_source.translation is not three finite numbers', '/frame/to_source');
      }
      offset = translation as ApmapVec3;
    } else if (transform?.type !== 'identity') {
      // 1.0 has only these two. Writing document coordinates and calling them source coordinates
      // would put the geometry somewhere nobody asked for, silently.
      throw new MapExportError('map_export_frame_unsupported', `frame.to_source.type ${JSON.stringify(transform?.type)} cannot be applied by this build`, '/frame/to_source');
    }
  }
  const quake3 = document.game === QUAKE3_GAME_NAME;
  if (quake3) quake3ExportPreflight(document);
  if (document.game === QUAKE2_GAME_NAME) quake2ExportPreflight(document);
  const lines: string[] = [];
  document.entities.forEach((entity, index) => entityBlock(entity, index, offset, resolved, lines, quake3));
  return `${lines.join('\n')}\n`;
}
