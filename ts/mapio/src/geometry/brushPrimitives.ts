/**
 * **Quake III brush primitives, as arithmetic** — `Q3_002`.
 *
 * A brushDef face (APMap 1.5 `projection.mode: 'brush_primitives'`, SEMANTICS §6.2a) says where its
 * texture goes with a 2x3 matrix applied in an axis base Q3Map2 derives from the face's plane:
 *
 * ```text
 * n     = normalize((p0 − p1) × (p2 − p1))          Q3Map2's PlaneFromPoints — the OUTWARD normal
 * texS, texT = ComputeAxisBase(n)                    after zeroing |component| < 1e-6
 * s = m00·dot(P, texS) + m01·dot(P, texT) + m02      in TEXTURE units: 1.0 is one repeat
 * t = m10·dot(P, texS) + m11·dot(P, texT) + m12
 * ```
 *
 * Every other module in this package speaks the classic and Valve 220 forms, and every one of them
 * divides by a texture's pixel size at the very end. Brush primitives do not — the matrix already
 * includes it — which is exactly why SEMANTICS forbids converting either way: a classic projection
 * becomes a matrix only given the image size, and a sheared matrix has no classic form at all. So
 * nothing here converts. What this module does instead is the one thing that needs no image size:
 * it reads a matrix as a pair of **world covectors** (a gradient and a constant per texture axis),
 * moves those covectors through an affine map exactly, and writes them back as a matrix in the moved
 * face's own axis base.
 *
 * ### The sign of the normal is load-bearing
 *
 * `projection.ts`'s `planeNormal` is `(p1 − p0) × (p2 − p0)`, the exact negative of Q3Map2's. For a
 * classic face the sign is irrelevant (the axis table keys on magnitudes); for a brushDef face it is
 * not — negating `n` negates `texS` and mirrors the texture. So everything here derives the normal
 * from the **plane points**, in Q3Map2's order, and never accepts one from a caller.
 */
import type { ApmapBrushPrimitivesProjection } from '../apmap/types.ts';

export type Vec3 = [number, number, number];
export type BrushPrimitivesMatrix = ApmapBrushPrimitivesProjection['matrix'];

/** One texture axis as a world covector: `value(P) = dot(P, gradient) + shift`. */
export type TextureCovector = { gradient: Vec3; shift: number };
export type TextureCovectors = { u: TextureCovector; v: TextureCovector };

const dot = (a: readonly number[], b: readonly number[]) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
const sub = (a: readonly number[], b: readonly number[]): Vec3 => [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
const cross = (a: readonly number[], b: readonly number[]): Vec3 =>
  [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];

/** Q3Map2's `ComputeAxisBase` zeroes components below this before taking angles. */
export const AXIS_BASE_EPSILON = 1e-6;

/**
 * Q3Map2's plane normal for three `.map` points — `(p0 − p1) × (p2 − p1)`, normalised. `undefined`
 * for collinear or coincident points, which have no plane at all.
 */
export function quake3PlaneNormal(points: readonly (readonly number[])[]): Vec3 | undefined {
  const raw = cross(sub(points[0], points[1]), sub(points[2], points[1]));
  const length = Math.hypot(raw[0], raw[1], raw[2]);
  if (!(length > 0) || !Number.isFinite(length)) return undefined;
  return [raw[0] / length, raw[1] / length, raw[2] / length];
}

/** Q3Map2's `ComputeAxisBase`: the texture plane's two world axes for an outward normal. */
export function quake3AxisBase(normal: readonly number[]): [Vec3, Vec3] {
  const n = normal.map((value) => (Math.abs(value) < AXIS_BASE_EPSILON ? 0 : value));
  const rotY = -Math.atan2(n[2], Math.sqrt(n[1] * n[1] + n[0] * n[0]));
  const rotZ = Math.atan2(n[1], n[0]);
  const texS: Vec3 = [-Math.sin(rotZ), Math.cos(rotZ), 0];
  const texT: Vec3 = [-Math.sin(rotY) * Math.cos(rotZ), -Math.sin(rotY) * Math.sin(rotZ), -Math.cos(rotY)];
  return [texS, texT];
}

/**
 * The world covectors a brushDef face's matrix means, or `undefined` for a face with no plane or a
 * non-finite matrix. Exact for every point in space, not only on the plane — `x'` and `y'` are plain
 * dot products with the axis base.
 */
export function brushPrimitivesCovectors(matrix: BrushPrimitivesMatrix, points: readonly (readonly number[])[]): TextureCovectors | undefined {
  const normal = quake3PlaneNormal(points);
  if (!normal) return undefined;
  if (!Array.isArray(matrix) || matrix.length !== 2 || !matrix.every((row) => Array.isArray(row) && row.length === 3 && row.every(Number.isFinite)))
    return undefined;
  const [texS, texT] = quake3AxisBase(normal);
  const axis = (row: readonly number[]): TextureCovector => ({
    gradient: [row[0] * texS[0] + row[1] * texT[0], row[0] * texS[1] + row[1] * texT[1], row[0] * texS[2] + row[1] * texT[2]],
    shift: row[2],
  });
  return { u: axis(matrix[0]), v: axis(matrix[1]) };
}

/**
 * The matrix that spells a pair of world covectors on the face with these plane points.
 *
 * Only the in-plane part of a gradient changes the texture ON the face: a point of the plane is
 * `x'·texS + y'·texT + d·n`, so the normal component contributes the constant `d·(g·n)`, which the
 * third column absorbs. That is what makes this exact for any covector pair, including one an affine
 * map pushed out of the plane.
 */
export function brushPrimitivesMatrixFor(covectors: TextureCovectors, points: readonly (readonly number[])[],
                                         number: (value: number, member: string) => number = (value) => value): BrushPrimitivesMatrix | undefined {
  const normal = quake3PlaneNormal(points);
  if (!normal) return undefined;
  const [texS, texT] = quake3AxisBase(normal);
  const distance = dot(normal, points[1]);
  const row = (axis: TextureCovector): [number, number, number] =>
    [number(dot(axis.gradient, texS), 'matrix'), number(dot(axis.gradient, texT), 'matrix'),
     number(axis.shift + distance * dot(axis.gradient, normal), 'matrix')];
  const matrix: BrushPrimitivesMatrix = [row(covectors.u), row(covectors.v)];
  return matrix.every((values) => values.every(Number.isFinite)) ? matrix : undefined;
}

/** `m00·m11 − m01·m10` — SEM-Q-3's determinant. Zero means the texture collapses to a line. */
export const brushPrimitivesDeterminant = (matrix: BrushPrimitivesMatrix): number =>
  matrix[0][0] * matrix[1][1] - matrix[0][1] * matrix[1][0];

/** Coefficients smaller than this after a transport are floating-point residue, and are written as 0. */
export const MATRIX_NOISE = 1e-12;

export type AffineMap = { linear: readonly [Vec3, Vec3, Vec3]; translation: Vec3 };

const applyLinear = (rows: readonly (readonly number[])[], vector: readonly number[]): Vec3 =>
  [dot(rows[0], vector), dot(rows[1], vector), dot(rows[2], vector)];

function inverse3(rows: readonly [Vec3, Vec3, Vec3]): [Vec3, Vec3, Vec3] | undefined {
  const [a, b, c] = rows;
  const det = dot(a, cross(b, c));
  if (!(Math.abs(det) > 1e-12) || !Number.isFinite(det)) return undefined;
  // The adjugate's rows are the cross products of the columns; transposed, they are these.
  const r0 = cross(b, c), r1 = cross(c, a), r2 = cross(a, b);
  return [[r0[0] / det, r1[0] / det, r2[0] / det], [r0[1] / det, r1[1] / det, r2[1] / det], [r0[2] / det, r1[2] / det, r2[2] / det]];
}

/** Determinant of the linear part. Negative means the map mirrors, and a mirrored face turns inside out. */
export const affineDeterminant = (transform: AffineMap): number =>
  dot(transform.linear[0], cross(transform.linear[1], transform.linear[2]));

export type BrushPrimitivesTransportRefusal = 'source_unprojectable' | 'transform_singular' | 'transform_mirrors' | 'non_finite';

/**
 * Carry a brushDef face's texture through `P → A·P + c`, exactly.
 *
 * The texture a point had before the move, it has after: `s'(P') = s(A⁻¹(P' − c))`, so the covector
 * gradient becomes `A⁻ᵀ·g` and the constant loses `(A⁻ᵀ·g)·c`. The face's new plane points are the
 * old ones moved the same way, **in the same order**, which is what every rigid tool in this package
 * emits — and it is why a mirroring map is refused rather than guessed at: a tool that mirrors
 * reorders its points to stay outward, and this function cannot know the order it chose.
 */
export function transportBrushPrimitives(projection: ApmapBrushPrimitivesProjection, sourcePoints: readonly (readonly number[])[],
                                         transform: AffineMap, number: (value: number, member: string) => number):
  { projection: ApmapBrushPrimitivesProjection } | { refusal: BrushPrimitivesTransportRefusal } {
  const covectors = brushPrimitivesCovectors(projection.matrix, sourcePoints);
  if (!covectors) return { refusal: 'source_unprojectable' };
  if (affineDeterminant(transform) < 0) return { refusal: 'transform_mirrors' };
  const inverse = inverse3(transform.linear);
  if (!inverse) return { refusal: 'transform_singular' };
  // `A⁻ᵀ·g`: the rows of `A⁻ᵀ` are the columns of `A⁻¹`.
  const inverseTranspose: [Vec3, Vec3, Vec3] = [[inverse[0][0], inverse[1][0], inverse[2][0]],
                                                [inverse[0][1], inverse[1][1], inverse[2][1]],
                                                [inverse[0][2], inverse[1][2], inverse[2][2]]];
  const move = (axis: TextureCovector): TextureCovector => {
    const gradient = applyLinear(inverseTranspose, axis.gradient);
    return { gradient, shift: axis.shift - dot(gradient, transform.translation) };
  };
  const targetPoints = sourcePoints.map((point) => {
    const moved = applyLinear(transform.linear, point);
    return [moved[0] + transform.translation[0], moved[1] + transform.translation[1], moved[2] + transform.translation[2]];
  });
  // A quarter turn is `cos 90° = 6e-17`, not 0, and the exact spelling of that residue is a 50-digit
  // number in the `.map` (measured on a browser export). Below `MATRIX_NOISE` a coefficient is
  // floating-point noise from the rotation, not an authored value, so it is written as 0 — a change
  // of at most 1e-12 of a texture repeat per unit, far under anything a renderer or Q3Map2 resolves.
  const snap = (value: number, member: string) => number(Math.abs(value) < MATRIX_NOISE ? 0 : value, member);
  const matrix = brushPrimitivesMatrixFor({ u: move(covectors.u), v: move(covectors.v) }, targetPoints, snap);
  if (!matrix) return { refusal: 'non_finite' };
  return { projection: { mode: 'brush_primitives', matrix } };
}

/** The texture coordinates of a point under a brushDef face's matrix — what the renderer and the tests read. */
export function brushPrimitivesTexCoord(matrix: BrushPrimitivesMatrix, points: readonly (readonly number[])[], point: readonly number[]): [number, number] | undefined {
  const covectors = brushPrimitivesCovectors(matrix, points);
  if (!covectors) return undefined;
  return [dot(point, covectors.u.gradient) + covectors.u.shift, dot(point, covectors.v.gradient) + covectors.v.shift];
}
