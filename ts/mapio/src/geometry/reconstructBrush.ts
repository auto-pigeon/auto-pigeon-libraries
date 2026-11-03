import type { ParsedBrush, ParsedFace } from '../map/types.ts';
import { planeNormal } from './projection.ts';

export type PolygonFace = { index: number; polygon: number[][]; triangle_indices: number[]; normal: number[]; texture_name: string; mapping: ParsedFace['mapping']; status: 'renderable' | 'redundant_non_contributing'; reason?: string };
type Plane = { n: number[]; d: number };
type Attempt = { sign: number; faces: PolygonFace[]; renderable: number; extent: number };
/**
 * How far outside a plane a point may sit and still be clipped as if it were on it, in Quake units.
 *
 * `qbsp`'s own figure, and the coarsest tolerance in the package by four orders of magnitude — a
 * hundredth of the smallest feature a Quake map can meaningfully have. Everything downstream of the
 * clipper inherits it whether it says so or not: a vertex within `0.01` of a plane is a vertex the
 * winding pass has already decided is *on* that plane, so no later stage can honestly claim to
 * resolve a distinction below it.
 *
 * Exported since `AUP 115` because the splitter has to classify which side of a plane the solid's
 * corners are on, and classifying at a tighter tolerance than the clipper resolves is how a "cut"
 * comes to produce a fragment the clipper then reports as having no volume. One number, named once.
 */
export const WINDING_EPSILON = .01;
const EPSILON = WINDING_EPSILON;
/**
 * The half-extent of the square every face's winding starts as, before the other planes clip it.
 *
 * Exported because it is the one number that says what "unbounded" means here: a plane set that does
 * not close leaves polygon vertices sitting on this box rather than on a neighbouring face, so
 * `convexity.ts` reads boundedness off the same clip this kernel already performs instead of running
 * a second, differently-approximated one.
 */
export const BASE_WINDING_SIZE = 65536;
const dot = (a: number[], b: number[]) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
const sub = (a: number[], b: number[]) => [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
const cross = (a: number[], b: number[]) => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];
const normalise = (value: number[]) => { const length = Math.hypot(...value); return length ? value.map((item) => item / length) : [0, 0, 0]; };
const clip = (polygon: number[][], plane: Plane, sign: number) => {
  const output: number[][] = [];
  for (let index = 0; index < polygon.length; index += 1) {
    const a = polygon[index], b = polygon[(index + 1) % polygon.length], da = sign * (dot(plane.n, a) - plane.d), db = sign * (dot(plane.n, b) - plane.d), insideA = da >= -EPSILON, insideB = db >= -EPSILON;
    if (insideA) output.push(a);
    if (insideA !== insideB) { const ratio = da / (da - db); output.push(a.map((value, axis) => value + (b[axis] - value) * ratio)); }
  }
  return output;
};
const unique = (points: number[][]) => points.filter((point, index) => !points.slice(0, index).some((other) => Math.hypot(...sub(point, other)) < EPSILON));
const attempt = (brush: ParsedBrush, planes: Plane[], sign: number): Attempt => {
  const faces = brush.faces.map((face, index): PolygonFace => {
    const plane = planes[index], helper = Math.abs(plane.n[2]) < .9 ? [0, 0, 1] : [0, 1, 0], axisU = normalise(cross(helper, plane.n)), axisV = normalise(cross(plane.n, axisU)), origin = plane.n.map((value) => value * plane.d);
    let polygon = [[-BASE_WINDING_SIZE, -BASE_WINDING_SIZE], [BASE_WINDING_SIZE, -BASE_WINDING_SIZE], [BASE_WINDING_SIZE, BASE_WINDING_SIZE], [-BASE_WINDING_SIZE, BASE_WINDING_SIZE]].map(([u, v]) => origin.map((value, axis) => value + axisU[axis] * u + axisV[axis] * v));
    for (let other = 0; other < planes.length && polygon.length; other += 1) if (other !== index) polygon = clip(polygon, planes[other], sign);
    polygon = unique(polygon);
    if (polygon.length < 3) return { index, polygon: [], triangle_indices: [], normal: plane.n, texture_name: face.textureName, mapping: face.mapping, status: 'redundant_non_contributing', reason: 'plane does not contribute to the selected convex shell' };
    return { index, polygon, triangle_indices: Array.from({ length: polygon.length - 2 }, (_, triangle) => [0, triangle + 1, triangle + 2]).flat(), normal: plane.n, texture_name: face.textureName, mapping: face.mapping, status: 'renderable' };
  });
  const points = faces.flatMap((face) => face.polygon), extent = points.length ? Math.max(...points.flat().map(Math.abs)) : Infinity;
  return { sign, faces, renderable: faces.filter((face) => face.status === 'renderable').length, extent };
};

/** Reconstruct a convex Quake brush by clipping a base winding against one globally oriented plane set. */
export function reconstructBrush(brush: ParsedBrush) {
  const planes = brush.faces.map((face) => { const n = normalise(cross(sub(face.points[1], face.points[0]), sub(face.points[2], face.points[0]))); return { n, d: dot(n, face.points[0]) }; });
  const candidates = [-1, 1].map((sign) => attempt(brush, planes, sign)).sort((a, b) => b.renderable - a.renderable || a.extent - b.extent || a.sign - b.sign);
  // `Q2_002A`: the clip above keeps its own planes, but the NORMAL a face reports is `planeNormal`'s — the one every
  // Classic face is projected through (`quakeFaceUvs`), and it must break a 45° tie exactly as `qbsp` does. The two
  // agree to the last bit or two; only a base-axis tie can tell them apart.
  const chosen = candidates[0];
  for (const face of chosen.faces) face.normal = planeNormal(brush.faces[face.index].points);
  const failed = chosen.faces.filter((face) => face.status !== 'renderable');
  return { faces: chosen.faces, failed, orientation_sign: chosen.sign, alternate_renderable_faces: candidates[1].renderable };
}
