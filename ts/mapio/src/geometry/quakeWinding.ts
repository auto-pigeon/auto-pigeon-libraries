/**
 * **Which way a face's three plane points make it face** — the one place in this package that
 * states Quake's own convention, and the only oracle that can see it (`AUP 140D`).
 *
 * ### The convention, and why it needed a file
 *
 * A `.map` face is three points. `qbsp` derives the plane from them as
 *
 * ```text
 * normal = (p0 − p1) × (p2 − p1)          and that normal points OUT of the solid
 * ```
 *
 * `projection.ts`'s {@link planeNormal} — the number every other module in this package means when
 * it says "the face's normal" — is `(p1 − p0) × (p2 − p0)`, which is the **exact negative** of it:
 *
 * ```text
 * (p0 − p1) × (p2 − p1) = −(p1 − p0) × ((p2 − p0) − (p1 − p0)) = −(p1 − p0) × (p2 − p0)
 * ```
 *
 * Two conventions, one package, and nothing that ever compared them. Every brush Auto-Pigeon
 * authored from nothing was written **inside out**: `cuboidFaces`' six-row table and
 * `outwardTriple` both ordered their points so that *`planeNormal`* pointed away from the solid,
 * which makes *`qbsp`'s* normal point into it. `qbsp` then intersects the complement of the brush,
 * finds no faces, prints `*** WARNING 09: Couldn't create brush faces` and keeps nothing —
 * which is exactly the incident `AUP 140` was opened for, at 216 brushes and 648 warnings.
 *
 * ### Why the product's own guard could not see it
 *
 * `reconstructBrush` derives an **orientation sign per brush** and picks whichever of the two
 * readings renders more faces, so it accepts either convention and draws both perfectly;
 * `checkBrushConvexity` is built on it and inherits the blindness; `map/writer.ts` is a verbatim
 * passthrough and normalises nothing. `createApmapBrush`'s documented crossing — *"checked rather
 * than trusted"* — was checking with the one oracle that cannot answer this question.
 *
 * So this module does **not** call `reconstructBrush` for the verdict. It reconstructs only to
 * obtain a point known to be inside the solid, and then asks each face independently which side of
 * itself that point is on. That is the prompt's *"orient every plane consistently against a known
 * interior point"*, and it is a second opinion rather than a restatement of the first.
 *
 * ### What is NOT here
 *
 * No repair on the way past. Nothing in this module is called at decode, at save, on a timer or
 * from `map/writer.ts`: a document's bytes are its author's, and silently rewinding somebody's
 * saved map at the export door is a migration claimed and not performed (`AUP 120J`'s rule).
 * {@link orientBrushToQuake} exists for the two callers that are *authoring* a brush this instant
 * and want the convention stated once instead of transcribed into a table.
 */
import type { ApmapVec3 } from '../apmap/types.ts';
import type { ParsedBrush } from '../map/types.ts';
import { BASE_WINDING_SIZE, reconstructBrush } from './reconstructBrush.ts';
import type { Vec3 } from './projection.ts';

/**
 * How far off a face's own plane the solid's interior point may sit and still be called "on it".
 *
 * The interior point is an average of reconstructed corners, so it inherits the clipper's own
 * `WINDING_EPSILON` (0.01) and nothing finer can be claimed about it. A solid whose centroid lands
 * this close to one of its own faces has no thickness there, and this module answers
 * `unreconstructable` rather than guessing which way that face points.
 */
export const WINDING_INTERIOR_EPSILON = 1e-6;

const sub = (a: readonly number[], b: readonly number[]): Vec3 => [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
const cross = (a: Vec3, b: Vec3): Vec3 => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];
const dot = (a: readonly number[], b: readonly number[]) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];

/**
 * The normal `qbsp` derives from a face's three plane points — `(p0 − p1) × (p2 − p1)`, normalised.
 *
 * The exact negative of {@link planeNormal}. Both are correct about the plane and they disagree
 * about which side of it is solid, which is the whole of this module's subject.
 */
export function quakePlaneNormal(points: readonly (readonly number[])[]): Vec3 {
  const n = cross(sub(points[0], points[1]), sub(points[2], points[1]));
  // `sqrt` of the sum, as `qbsp` normalises: `Math.hypot` can differ in the last bit, and a 45° tie is decided there (`Q2_002A`).
  const length = Math.sqrt(n[0] * n[0] + n[1] * n[1] + n[2] * n[2]);
  return length ? [n[0] / length, n[1] / length, n[2] / length] : [0, 0, 0];
}

/** The same three points in the order that gives the opposite normal. Its own inverse. */
export function reverseWinding<T extends readonly [ApmapVec3, ApmapVec3, ApmapVec3]>(points: T): [ApmapVec3, ApmapVec3, ApmapVec3] {
  return [points[2], points[1], points[0]];
}

export type BrushWinding = 'outward' | 'inward' | 'mixed' | 'unreconstructable';

/** A face's own verdict, plus the numbers behind it, for a report that has to name the face. */
export type FaceWinding = { index: number; winding: 'outward' | 'inward' | 'degenerate'; signedDistance: number };

export type BrushWindingReport = {
  winding: BrushWinding;
  /** Empty when the brush could not be reconstructed at all. */
  faces: FaceWinding[];
  /** The point every face was measured against, when there was one. */
  interior?: Vec3;
  /** Why `unreconstructable`, when it is. */
  reason?: 'no_faces' | 'no_solid' | 'degenerate_face' | 'interior_on_face';
};

type WindingFace = { plane: { points: readonly [ApmapVec3, ApmapVec3, ApmapVec3] | readonly ApmapVec3[] } };
type WindingBrush = { faces: readonly WindingFace[] };

/** A `ParsedBrush` view of an APMap brush, so the one reconstruction kernel can be reused. */
const parsedFor = (brush: WindingBrush): ParsedBrush => ({
  index: 0,
  faces: brush.faces.map((face, index) => ({
    index, points: face.plane.points.map((point) => [...point]) as number[][],
    textureName: '', mapping: { shift: [0, 0], rotation: 0, scale: [1, 1] },
  })) as unknown as ParsedBrush['faces'],
} as ParsedBrush);

/**
 * Which way this brush's declared planes face, measured against a point inside its own solid.
 *
 * `outward` is what a `.map` must contain. `inward` is a brush `qbsp` discards whole. `mixed` is
 * worse than either: the solid the faces claim is not a solid at all, and it is what the `AUP 140C`
 * validator called `winding_mixed` on two of TEXT's glyph brushes.
 */
export function brushWindingReport(brush: WindingBrush): BrushWindingReport {
  if (!brush.faces.length) return { winding: 'unreconstructable', faces: [], reason: 'no_faces' };
  const reconstructed = reconstructBrush(parsedFor(brush));
  const corners = reconstructed.faces.flatMap((face) => face.polygon);
  if (corners.length < 4) return { winding: 'unreconstructable', faces: [], reason: 'no_solid' };
  // **An unbounded plane set has no interior to measure against.** `reconstructBrush` clips a
  // 65 536-unit base winding, so a corner still sitting on that box means the planes never closed —
  // `convexity.ts` reads boundedness off the same clip, and this is the same reading. Without it a
  // brush with one reversed face reports `outward`, because the "solid" it settles to is the far
  // side of the world and every plane faces away from its centre.
  if (corners.some((corner) => corner.some((value) => Math.abs(value) >= BASE_WINDING_SIZE - 1)))
    return { winding: 'unreconstructable', faces: [], reason: 'no_solid' };
  const interior: Vec3 = [0, 0, 0];
  for (const corner of corners) { interior[0] += corner[0] / corners.length; interior[1] += corner[1] / corners.length; interior[2] += corner[2] / corners.length; }

  const faces: FaceWinding[] = [];
  let reason: BrushWindingReport['reason'];
  for (let index = 0; index < brush.faces.length; index += 1) {
    const points = brush.faces[index].plane.points;
    const normal = quakePlaneNormal(points);
    if (!normal[0] && !normal[1] && !normal[2]) { faces.push({ index, winding: 'degenerate', signedDistance: 0 }); reason ??= 'degenerate_face'; continue; }
    // Positive means the interior sits on the side the declared normal points at — which is the
    // side `qbsp` will treat as empty. That is the inside-out reading.
    const signedDistance = dot(normal, interior) - dot(normal, points[1]);
    if (Math.abs(signedDistance) <= WINDING_INTERIOR_EPSILON) { faces.push({ index, winding: 'degenerate', signedDistance }); reason ??= 'interior_on_face'; continue; }
    faces.push({ index, winding: signedDistance < 0 ? 'outward' : 'inward', signedDistance });
  }
  if (reason) return { winding: 'unreconstructable', faces, interior, reason };
  const outward = faces.filter((face) => face.winding === 'outward').length;
  return { winding: outward === faces.length ? 'outward' : outward === 0 ? 'inward' : 'mixed', faces, interior };
}

/** Shorthand for {@link brushWindingReport}'s verdict. */
export const brushWinding = (brush: WindingBrush): BrushWinding => brushWindingReport(brush).winding;

/**
 * The brush with every face's triple wound the way `qbsp` requires.
 *
 * **Byte-identical, object-identical, when the brush is already right** — a face that needs no
 * change is returned as the same object, so a caller can tell whether anything moved by reference.
 * A brush that cannot be reconstructed is returned untouched: there is no interior point to orient
 * against, and inventing one would be a repair nobody could check.
 */
export function orientBrushToQuake<B extends { faces: readonly F[] }, F extends { plane: { points: [ApmapVec3, ApmapVec3, ApmapVec3] } }>(brush: B): B {
  const report = brushWindingReport(brush as unknown as WindingBrush);
  if (report.winding === 'outward' || report.winding === 'unreconstructable') return brush;
  const faces = brush.faces.map((face, index) =>
    report.faces[index]?.winding === 'inward' ? { ...face, plane: { ...face.plane, points: reverseWinding(face.plane.points) } } : face);
  return { ...brush, faces };
}
