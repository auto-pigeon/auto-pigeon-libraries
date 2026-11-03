/**
 * Quake's texture-projection axes, as arithmetic, in the core.
 *
 * These two rules — which world plane a classic face projects onto, and how `rotation` turns those
 * axes in the texture plane — lived in `three/uv.ts`, because until now only the renderer needed
 * them. Translation needs them too: moving a brush without compensating its texture shift slides
 * the texture across the geometry, and the compensation is a projection of the movement onto
 * *exactly these* axes.
 *
 * So they moved here rather than being copied. Two implementations of "which axis does this face
 * project onto" would agree on the day they were written and disagree the first time either was
 * touched, and the symptom — a brush whose texture creeps a few texels every time it is nudged —
 * would be attributed to anything but a duplicated constant.
 *
 * Nothing here imports Three.js or anything else. `three/uv.ts` now imports from this file.
 */

import type { ApmapQuakeProjection } from '../apmap/types.ts';

export type Vec3 = [number, number, number];

const dot3 = (a: readonly number[], b: readonly number[]) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];

/**
 * Whose arithmetic decides which world plane a Classic face projects onto (`NEW_325B`).
 *
 * A Classic face stores five numbers and no axes: every reader re-derives the base plane from the face's
 * normal, so *which reader* is part of what the five numbers mean. The two compilers Auto-Pigeon writes
 * for agree on the table and on its order (floor/ceiling, then the X walls, then the Y walls) and differ in
 * how a later row replaces an earlier one:
 *
 * - `quake` — `qbsp`: double precision, and a later row wins the moment it is strictly larger.
 * - `quake3` — Q3Map2 (`TextureAxisFromPlane`, read at `2.5.17n-git-68ecbed`): single precision, and a later
 *   row wins only when it is larger by MORE than 0.0001. A normal whose X exceeds its Z by 2e-10 is a floor
 *   to Q3Map2 and a wall to `qbsp`.
 *
 * The difference is a band 1e-4 wide beside each exact tie, and inside it the two readers draw the same
 * five numbers through different planes — measured as half a texture repeat on two triangles of a deformed
 * floor (`DESIGN.md` §129.2). It is a property of the DOCUMENT's game and is never stored on a face:
 * `classicReaderOf(document)` is the one place that decides it.
 */
export type ClassicReader = 'quake' | 'quake3';

/** The reader of a document's Classic faces. Everything that is not Quake III is read as `qbsp` reads it. */
export const classicReaderOf = (document: { game?: unknown } | null | undefined): ClassicReader =>
  document?.game === 'quake3' ? 'quake3' : 'quake';

const QUAKE_AXES: readonly [Vec3, Vec3][] = [[[1, 0, 0], [0, -1, 0]], [[0, 1, 0], [0, 0, -1]], [[1, 0, 0], [0, 0, -1]]];
/** Q3Map2 zeroes a normal component smaller than this before it looks at the table. */
const Q3_NORMAL_SNAP = 0.00001;
/** How much larger a later row of the table must be for Q3Map2 to prefer it. */
const Q3_AXIS_MARGIN = Math.fround(0.0001);

/**
 * Q3Map2's choice, in its own precision: the unit normal narrowed to single precision, near-zero components
 * dropped (and the rest re-normalised) as its plane snapping does, then Z, X, Y visited in that order with a
 * later one taking over only past the margin. Both signs of an axis are one row here because the table's two
 * rows for an axis carry the same pair.
 *
 * What this cannot reproduce: Q3Map2 shares one stored plane between faces whose normals agree within 1e-5,
 * so a face whose two largest components differ by the margin to within about 2e-5 may be read through the
 * normal of ANOTHER brush's plane. No arithmetic on one face can know that.
 */
function quake3ClassicRow(normal: readonly number[]): 0 | 1 | 2 {
  const length = Math.hypot(normal[0], normal[1], normal[2]);
  if (!(length > 0) || !Number.isFinite(length)) return 0;
  let n = [Math.fround(normal[0] / length), Math.fround(normal[1] / length), Math.fround(normal[2] / length)];
  if (n.some((value) => value !== 0 && Math.abs(value) < Q3_NORMAL_SNAP)) {
    n = n.map((value) => (Math.abs(value) < Q3_NORMAL_SNAP ? 0 : value));
    const snapped = Math.hypot(n[0], n[1], n[2]);
    n = n.map((value) => Math.fround(value / snapped));
  }
  let best = 0, row: 0 | 1 | 2 = 0;
  for (const [candidate, component] of [[0, 2], [1, 0], [2, 1]] as const) {
    const magnitude = Math.abs(n[component]);
    if (magnitude > Math.fround(best + Q3_AXIS_MARGIN)) { best = magnitude; row = candidate; }
  }
  return row;
}

/**
 * The classic base axes: the world plane most nearly parallel to the face, as `reader` decides it.
 *
 * `v` points down in both cases because Quake's texture space has its origin at the top-left of the
 * image.
 */
export function classicAxes(normal: readonly number[], reader: ClassicReader): [Vec3, Vec3] {
  if (reader === 'quake3') { const [u, v] = QUAKE_AXES[quake3ClassicRow(normal)]; return [[u[0], u[1], u[2]], [v[0], v[1], v[2]]]; }
  const x = Math.abs(normal[0]), y = Math.abs(normal[1]), z = Math.abs(normal[2]);
  if (z >= x && z >= y) return [[1, 0, 0], [0, -1, 0]];
  if (x >= y) return [[0, 1, 0], [0, 0, -1]];
  return [[1, 0, 0], [0, 0, -1]];
}

/** Turn a pair of projection axes by `degrees` in the texture plane. Identity at 0. */
export function rotateProjectionAxes(uAxis: Vec3, vAxis: Vec3, degrees: number): [Vec3, Vec3] {
  const radians = ((degrees ?? 0) * Math.PI) / 180;
  if (!radians) return [uAxis, vAxis];
  const cosine = Math.cos(radians), sine = Math.sin(radians);
  return [
    [uAxis[0] * cosine - vAxis[0] * sine, uAxis[1] * cosine - vAxis[1] * sine, uAxis[2] * cosine - vAxis[2] * sine],
    [uAxis[0] * sine + vAxis[0] * cosine, uAxis[1] * sine + vAxis[1] * cosine, uAxis[2] * sine + vAxis[2] * cosine],
  ];
}

/**
 * The unit normal of a face's three plane points, in the winding order the kernel uses.
 *
 * `Q2_002A`: computed so that every component is **bit for bit** the negation of the normal `qbsp` derives
 * (`common/mapfile.cc` `parse_brush_face`: `(p0 − p1) × (p2 − p1)`, divided by `sqrt(x² + y² + z²)`), because
 * a Classic face on an exact 45° plane is a tie in {@link classicAxes} and a tie is decided by the last bit.
 * `(b − a) × (c − b)` is mathematically the same vector as `(b − a) × (c − a)`, the winding this kernel has
 * always used, but it is computed from `qbsp`'s own two differences, negated exactly, and `Math.hypot` is not
 * `sqrt` of the sum: either difference alone drew one authored exact-tie face in about fifteen hundred on a
 * different base plane than the compiler (`classicTie.test.ts`).
 */
export function planeNormal(points: readonly (readonly number[])[]): Vec3 {
  const a = points[0], b = points[1], c = points[2];
  const u = [b[0] - a[0], b[1] - a[1], b[2] - a[2]], v = [c[0] - b[0], c[1] - b[1], c[2] - b[2]];
  const n: Vec3 = [u[1] * v[2] - u[2] * v[1], u[2] * v[0] - u[0] * v[2], u[0] * v[1] - u[1] * v[0]];
  const length = Math.sqrt(n[0] * n[0] + n[1] * n[1] + n[2] * n[2]);
  return length ? [n[0] / length, n[1] / length, n[2] / length] : [0, 0, 0];
}

/**
 * How far a texture shift must move to keep the same texel on the same piece of surface when that
 * surface moves by `delta`.
 *
 * `quakeFaceUvs` computes `u = (dot(P, uAxis) / scaleU + uShift) / width`. For the moved point
 * `P + delta` to land on the same `u`, the shift must absorb the movement's component along the
 * axis, divided by the scale — so the answer is a *subtraction*, and it is the only place in this
 * package that knows that. Both the APMap document and the reconstructed scene faces compensate
 * through this one function.
 *
 * A zero or non-finite scale is left alone rather than dividing by it: such a face cannot be
 * projected at all and `quakeProjection` already refuses it by name. Silently substituting 1 here
 * would move the shift by an amount nobody authored.
 */
export function shiftCompensation(axis: readonly number[], delta: readonly number[], scale: number): number {
  if (!Number.isFinite(scale) || scale === 0) return 0;
  return -dot3(axis, delta) / scale;
}

/**
 * The projection axes a face actually projects along, in world space.
 *
 * Valve 220 records them; classic derives them from the normal and then turns them by `rotation`.
 * It lives here, beside the two rules it is composed of, because since `AUP 136` four different
 * things need the identical answer for the identical face — the transport authority, the scene-side
 * translation helper, the diagnostics and the renderer — and a second implementation of "which axis
 * does this face project onto" would agree on the day it was written and disagree the first time
 * either was touched. `translate.ts` re-exports it for the callers that have always imported it there.
 *
 * Classic and Valve 220 only. A brushDef face's axes depend on the SIGN of its plane normal, which a
 * normal handed in from elsewhere does not reliably carry — `brushPrimitives.ts` derives them from the
 * face's points instead, and the type keeps a brushDef projection from reaching this function.
 */
export function faceProjectionAxes(projection: ApmapQuakeProjection, normal: readonly number[], reader: ClassicReader): [Vec3, Vec3] {
  if (projection.mode === 'valve220') {
    return [[projection.u_axis[0], projection.u_axis[1], projection.u_axis[2]],
            [projection.v_axis[0], projection.v_axis[1], projection.v_axis[2]]];
  }
  return rotateProjectionAxes(...classicAxes(normal, reader), projection.rotation ?? 0);
}
