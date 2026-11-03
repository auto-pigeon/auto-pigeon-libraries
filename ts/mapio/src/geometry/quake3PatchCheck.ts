/**
 * **What a Quake III patch must satisfy before it is stored or written** — the structural half of
 * `Q3_003`'s patch module, the part every reader and writer of the format needs (SEMANTICS §9c,
 * SEM-Q-2). Tessellation, editing and preview live with the editor that draws a patch; this file
 * holds no geometry beyond the control-net area the degenerate check reads.
 */
import type { ApmapPatchItem } from '../apmap/types.ts';

/** SEMANTICS §9c: each dimension odd, 3…31 (Q3Map2 refuses 33 — `ParsePatch: bad size`; the engine refuses even). */
export const MIN_PATCH_DIMENSION = 3;
export const MAX_PATCH_DIMENSION = 31;
/** APMap 1.5's bound on every control-point component — Q3Map2's `MAX_WORLD_COORD`. */
export const MAX_PATCH_COMPONENT = 131072;

export type PatchPoint = [number, number, number, number, number];

export const isOddPatchDimension = (value: unknown): value is number =>
  typeof value === 'number' && Number.isInteger(value) && value >= MIN_PATCH_DIMENSION && value <= MAX_PATCH_DIMENSION && value % 2 === 1;

// ---- validation ---------------------------------------------------------------------------------

export type PatchFault =
  | 'dimension_invalid'      // width/height not odd 3…31
  | 'grid_mismatch'          // control_points is not width columns of height points (SEM-Q-2)
  | 'point_invalid'          // a point is not five numbers
  | 'non_finite'             // NaN / Infinity
  | 'out_of_range'           // a component beyond ±131072
  | 'degenerate'             // the net spans no area: every point coincident or collinear
  | 'texture_invalid';       // empty, or cannot be written to a .map

export type PatchCheck = { ok: true } | { ok: false; fault: PatchFault; detail: string };

/**
 * Everything a patch must satisfy **before** it is committed or written — the refusal a tool meets
 * at its entry, so a rejected edit leaves the document and the history unchanged.
 *
 * `structural` skips the one check that is about *intent* rather than validity: a net collapsed to a
 * line or a point. Real maps carry such patches — `Q3_003` measured 134 of 402 in one private source,
 * and Q3Map2 builds them — so the `.map` writer and every carry path accept one that was already
 * there, and only an EDIT is refused for collapsing a net (`state/patchCommands.ts`).
 */
export function checkPatch(patch: Pick<ApmapPatchItem, 'width' | 'height' | 'control_points' | 'texture'>, options: { structural?: boolean } = {}): PatchCheck {
  const { width, height } = patch;
  if (!isOddPatchDimension(width) || !isOddPatchDimension(height))
    return { ok: false, fault: 'dimension_invalid', detail: `${String(width)}x${String(height)}: each dimension must be odd, ${MIN_PATCH_DIMENSION} to ${MAX_PATCH_DIMENSION}` };
  if (typeof patch.texture !== 'string' || !/^[^\s"]+$/.test(patch.texture))
    return { ok: false, fault: 'texture_invalid', detail: JSON.stringify(patch.texture) };
  const columns = patch.control_points;
  if (!Array.isArray(columns) || columns.length !== width || columns.some((column) => !Array.isArray(column) || column.length !== height))
    return { ok: false, fault: 'grid_mismatch', detail: `${width}x${height}` };
  for (let c = 0; c < width; c += 1)
    for (let r = 0; r < height; r += 1) {
      const point = columns[c][r];
      if (!Array.isArray(point) || point.length !== 5 || point.some((value) => typeof value !== 'number'))
        return { ok: false, fault: 'point_invalid', detail: `${c},${r}` };
      if (point.some((value) => !Number.isFinite(value))) return { ok: false, fault: 'non_finite', detail: `${c},${r}` };
      if (point.some((value) => Math.abs(value) > MAX_PATCH_COMPONENT)) return { ok: false, fault: 'out_of_range', detail: `${c},${r}` };
    }
  if (!options.structural && patchNetArea(columns) <= 1e-6) return { ok: false, fault: 'degenerate', detail: `${width}x${height}` };
  return { ok: true };
}

/** The area the control net spans, summed over its quads. Zero for a net collapsed to a line or a point. */
export function patchNetArea(columns: readonly (readonly (readonly number[])[])[]): number {
  let area = 0;
  for (let c = 0; c + 1 < columns.length; c += 1)
    for (let r = 0; r + 1 < columns[c].length; r += 1) {
      const a = columns[c][r], b = columns[c + 1][r], d = columns[c][r + 1], e = columns[c + 1][r + 1];
      const p = [e[0] - a[0], e[1] - a[1], e[2] - a[2]], q = [d[0] - b[0], d[1] - b[1], d[2] - b[2]];
      const cross = [p[1] * q[2] - p[2] * q[1], p[2] * q[0] - p[0] * q[2], p[0] * q[1] - p[1] * q[0]];
      area += Math.hypot(cross[0], cross[1], cross[2]) / 2;
    }
  return area;
}
