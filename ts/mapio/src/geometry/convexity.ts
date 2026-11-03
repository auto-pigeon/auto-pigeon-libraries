/**
 * Does this brush's face-plane set bound a valid, closed, non-degenerate convex solid?
 *
 * ### The question is not the one it sounds like
 *
 * An intersection of half-spaces is convex *by definition*. There is no plane set anywhere whose
 * intersection is a non-convex region, so "check that this brush is convex" cannot mean what it
 * reads as, and an implementation that returns `true` for everything would be technically correct
 * and useless. The checkable question — the one a CSG repair loop actually needs answered — is
 * whether the plane set bounds a solid **that is the solid its faces claim**:
 *
 * - non-empty: the half-spaces do not contradict each other;
 * - bounded: the region closes rather than running off to infinity;
 * - non-degenerate: it encloses volume rather than collapsing to a sheet, a line or a point;
 * - complete: every declared face plane actually carries a facet of that solid.
 *
 * The last one is what catches a non-convex *authored* shape. Write an L as eight planes and the
 * intersection of those eight half-spaces is a box in the L's inner corner; two of the eight planes
 * then bound nothing at all. That is `not_convex`, and the offending face indices are returned,
 * because "which faces" is the first thing a repair pass needs and a boolean cannot say it.
 *
 * ### One kernel, not two
 *
 * `reconstructBrush` already orients the plane set and clips a base winding against it. This module
 * calls it rather than re-deriving either: the orientation it picked is the orientation checked
 * here, and boundedness is read off the same clip. A second, differently-approximated winding pass
 * would eventually disagree with the renderer about the same brush, and a diagnostic that disagrees
 * with what is on screen is worse than none.
 *
 * The vertices themselves come from plane triples rather than from that clip, because the clip's
 * output is by construction inside every half-space (it was produced by clipping against them) and
 * could never report a violation. Triple intersections are independent of it, which is what makes
 * the check a check.
 *
 * ### Nothing here runs on its own
 *
 * AULIBS's first product rule, which this code is written to live under: validation is on-demand
 * only. Nothing calls this at load, at save, on a timer or in the background; it answers when asked
 * and returns a value. It also neither reads nor writes APMap 1.1's `broken` flag — a caller that
 * wants both facts asks for this result and reads that member, and decides for itself. A brush that
 * fails here is not thereby broken, dropped, repaired or refused.
 */
import type { ParsedBrush, ParsedMap } from '../map/types.ts';
import { BASE_WINDING_SIZE, reconstructBrush } from './reconstructBrush.ts';

/**
 * How far outside a plane a point may sit and still count as on it, in Quake units.
 *
 * **Measured, not chosen.** Swept over the frozen clean APMap corpus — 38 whole Quake maps,
 * **42,994 brushes**, at most 12 faces each — asking how far outside its own planes a genuine corner
 * of the reconstructed solid lands:
 *
 * | | |
 * | --- | --- |
 * | worst deviation over all 42,994 brushes | `5.093e-11` (`e3m5.apmap` `e0.b1775`, 7 faces) |
 * | runners-up | `4.48e-11` (`e2m7` `e0.b1161`), `3.64e-11` (`e2m7` `e0.b1159`, `e2m7` `e0.b1180`) |
 * | median, and brushes above `0.01` | `0`, and none |
 * | tolerance at which corpus brushes start failing | `1e-12` — 24 of them |
 * | tolerances at which none fail | `1e-9`, `1e-7`, `1e-6`, `1e-5`, `1e-3` |
 *
 * So the honest floor is between `1e-12` and `1e-9` and is pure float noise at ±4096 world
 * coordinates, not anything geometric. `1e-6` sits in the middle of the clean band rather than at
 * its edge: ~20,000× the worst measured deviation, and still 10,000× *tighter* than the `EPSILON =
 * 0.01` that `reconstructBrush` and `qbsp` treat as "on the plane". It cannot mask a real fault —
 * the smallest meaningful feature in a Quake map is one unit — and it will not fail a brush over
 * arithmetic.
 *
 * The seven-face brushes at the top of that table are the ones to watch when this is revisited:
 * they are the corpus's tightest cases, and a future CSG pass emitting geometry near that deviation
 * is the first thing that would make this constant matter.
 */
export const CONVEXITY_TOLERANCE = 1e-6;

/**
 * Above this many faces the check refuses rather than running.
 *
 * Vertex enumeration is O(n³) triples each tested against n planes, so cost grows as n⁴: 12 faces is
 * 220 triples, 64 is 41,664, 256 would be 2.7 million. The corpus maximum is **12**, and a document
 * is untrusted input that may hold anything — a library that hangs on a hostile brush is a worse
 * outcome than one that says it declined.
 */
export const MAX_CONVEXITY_FACES = 64;

export type BrushConvexityReason =
  /** Fewer than four half-spaces cannot bound a volume. */
  | 'too_few_faces'
  /** A face's three points are identical or collinear, so it defines no plane. */
  | 'degenerate_plane'
  /** More faces than `MAX_CONVEXITY_FACES`; not checked rather than checked slowly. */
  | 'too_many_faces'
  /** The region does not close — it runs to infinity in some direction. */
  | 'unbounded'
  /** The half-spaces contradict each other: no point satisfies them all. */
  | 'empty'
  /** Closed and bounded, but encloses no volume: a sheet, an edge or a point. */
  | 'degenerate'
  /** Some declared face plane carries no facet of the solid the planes bound. */
  | 'not_convex';

export type BrushConvexity = {
  ok: boolean;
  /** Absent exactly when `ok`. */
  reason?: BrushConvexityReason;
  /** One sentence naming what was found. Diagnostic text, not a user-facing string. */
  detail?: string;
  /** The face indices responsible, where they are determinable. Empty when they are not. */
  faces: number[];
  /** Corners of the bounded solid, as counted by this check. */
  vertexCount: number;
  /** Enclosed volume in cubic Quake units; `0` for every failure that precedes the volume test. */
  volume: number;
  /** Which way the plane set was read, from `reconstructBrush`. Recorded so a result is reproducible. */
  orientationSign: number;
};

export type BrushConvexityOptions = {
  /** Defaults to `CONVEXITY_TOLERANCE`. Passing a different one is a deliberate act; see its note. */
  tolerance?: number;
};

const dot = (a: number[], b: number[]) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
const sub = (a: number[], b: number[]) => [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
const cross = (a: number[], b: number[]) => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];

/** Below this, three plane normals are too near coplanar for their intersection to be a point. */
const SINGULAR = 1e-9;

type Plane = { n: number[]; d: number };

const fail = (reason: BrushConvexityReason, detail: string, faces: number[], orientationSign: number,
              vertexCount = 0, volume = 0): BrushConvexity =>
  ({ ok: false, reason, detail, faces, vertexCount, volume, orientationSign });

/**
 * Check one brush. Never throws: a brush is untrusted input, and a thrown error inside a Web Worker
 * is an anonymous failure with nothing in it a reviewer can act on.
 */
/**
 * The volume a set of reconstructed face polygons encloses, in cubic Quake units.
 *
 * Fan-triangulated per polygon about the origin — the divergence theorem, so a closed surface's
 * enclosed volume falls out of its faces without the solid having to be decomposed. The sign follows
 * the winding, so the magnitude is the answer and the orientation is reported separately by whoever
 * asked; every caller here wants the magnitude.
 *
 * Exported because `geometry/csg.ts` needs the volume of a brush it is about to carve, and running a
 * whole `checkBrushConvexity` — O(n⁴) in the face count — to read one number off the end of it is
 * the kind of second implementation this repository keeps paying for elsewhere. One formula, two
 * callers.
 */
export function enclosedVolume(faces: ReadonlyArray<{ polygon: number[][] }>): number {
  let volume = 0;
  for (const face of faces)
    for (let index = 1; index + 1 < face.polygon.length; index += 1)
      volume += dot(face.polygon[0], cross(face.polygon[index], face.polygon[index + 1])) / 6;
  return Math.abs(volume);
}

export function checkBrushConvexity(brush: ParsedBrush, options: BrushConvexityOptions = {}): BrushConvexity {
  const tolerance = options.tolerance ?? CONVEXITY_TOLERANCE;
  const faces = brush.faces ?? [];
  if (faces.length < 4)
    return fail('too_few_faces', `a closed convex solid needs at least four half-spaces; this brush declares ${faces.length}`, [], 0);
  if (faces.length > MAX_CONVEXITY_FACES)
    return fail('too_many_faces', `${faces.length} faces is past the ${MAX_CONVEXITY_FACES} this check will enumerate vertices for`, [], 0);

  // Signed planes in the source's own winding. Orientation is applied afterwards, in one place.
  const signed: Plane[] = [];
  const degenerate: number[] = [];
  for (const [index, face] of faces.entries()) {
    const raw = cross(sub(face.points[1], face.points[0]), sub(face.points[2], face.points[0]));
    const length = Math.hypot(...raw);
    if (!Number.isFinite(length) || length <= SINGULAR) { degenerate.push(index); continue; }
    const n = raw.map((value) => value / length);
    signed.push({ n, d: dot(n, face.points[0]) });
  }
  if (degenerate.length)
    return fail('degenerate_plane', `face${degenerate.length > 1 ? 's' : ''} ${degenerate.join(', ')} declare${degenerate.length > 1 ? '' : 's'} three identical or collinear points, so no plane`, degenerate, 0);

  const built = reconstructBrush(brush);
  const orientationSign = built.orientation_sign;
  // The clipper keeps `sign * (n·p − d) ≥ 0`, so *inside* is the positive side of the signed plane
  // and the outward normal — the one a half-space test wants — is its negation.
  const planes: Plane[] = signed.map((plane) => ({ n: plane.n.map((value) => -orientationSign * value), d: -orientationSign * plane.d }));
  const outside = (point: number[]) => planes.reduce((worst, plane) => Math.max(worst, dot(plane.n, point) - plane.d), -Infinity);

  const vertices: number[][] = [];
  for (let i = 0; i < planes.length; i += 1)
    for (let j = i + 1; j < planes.length; j += 1)
      for (let k = j + 1; k < planes.length; k += 1) {
        const [a, b, c] = [planes[i], planes[j], planes[k]];
        const determinant = dot(a.n, cross(b.n, c.n));
        if (Math.abs(determinant) < SINGULAR) continue;   // near-parallel: no single intersection
        const bc = cross(b.n, c.n), ca = cross(c.n, a.n), ab = cross(a.n, b.n);
        const point = bc.map((value, axis) => (value * a.d + ca[axis] * b.d + ab[axis] * c.d) / determinant);
        if (point.every(Number.isFinite) && outside(point) <= tolerance) vertices.push(point);
      }

  // Boundedness off the kernel's own clip: a plane set that does not close leaves winding vertices
  // sitting on the base square instead of on a neighbouring face.
  //
  // **Checked before emptiness, not after**, which is not the obvious order. An unbounded region can
  // have no vertices at all — a square tube of four vertical planes has every plane triple singular,
  // so vertex enumeration finds nothing — and reporting that as "the planes contradict each other"
  // would be a confident wrong answer about a region that is in fact infinite and non-empty.
  const escapes = built.faces.filter((face) =>
    face.polygon.some((point) => point.some((value) => Math.abs(value) >= BASE_WINDING_SIZE - 1)));
  if (escapes.length)
    return fail('unbounded', 'the plane set does not close: it bounds a region that runs to infinity',
                escapes.map((face) => face.index), orientationSign, vertices.length);

  if (!vertices.length)
    return fail('empty', 'no point satisfies every half-space: the planes contradict each other', [], orientationSign);

  const volume = enclosedVolume(built.faces);
  if (volume <= tolerance)
    return fail('degenerate', `the plane set closes but encloses no volume (${volume}); it collapses to a sheet, an edge or a point`, [], orientationSign, vertices.length, volume);

  // Completeness: three vertices is the least that bounds a facet, so a plane with fewer carries none.
  const bare = planes
    .map((plane, index) => ({ index, on: vertices.filter((point) => Math.abs(dot(plane.n, point) - plane.d) <= tolerance).length }))
    .filter((face) => face.on < 3);
  if (bare.length)
    return fail('not_convex',
                `face${bare.length > 1 ? 's' : ''} ${bare.map((face) => face.index).join(', ')} bound${bare.length > 1 ? '' : 's'} no part of the solid these planes describe, so the faces do not describe one convex solid`,
                bare.map((face) => face.index), orientationSign, vertices.length, volume);

  return { ok: true, faces: [], vertexCount: vertices.length, volume, orientationSign };
}

export type DocumentConvexityEntry = {
  entityIndex: number;
  brushIndex: number;
  /** The brush's APMap id when it has one — a `.map` brush has none. */
  brushId: string | null;
  result: BrushConvexity;
};

/**
 * Every brush in a parsed document, checked.
 *
 * A convenience over `checkBrushConvexity` and deliberately nothing more: it maps, it does not
 * summarise, and there is no `isDocumentValid` boolean anywhere in this package for a caller to
 * reach for by accident. Whether a map with three failing brushes is a problem is the caller's
 * judgement, and usually the answer is that somebody is still working on it.
 */
export function checkDocumentConvexity(map: ParsedMap, options: BrushConvexityOptions = {}): DocumentConvexityEntry[] {
  const entries: DocumentConvexityEntry[] = [];
  for (const [entityIndex, entity] of map.entities.entries())
    for (const [brushIndex, brush] of entity.brushes.entries())
      entries.push({ entityIndex, brushIndex, brushId: brush.stableId ?? null, result: checkBrushConvexity(brush, options) });
  return entries;
}
