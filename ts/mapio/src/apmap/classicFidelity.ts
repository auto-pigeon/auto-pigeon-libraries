/**
 * **A Classic face's texture survives SER-7, or the face is named** — `Q2_002A`.
 *
 * A Classic face stores five numbers and no axes: the compiler re-derives the base plane from the face's
 * three points (`classicAxes`). SER-7 rounds those points, and the five numbers, to six decimal places. Two
 * things then go wrong that no single number's rounding error explains:
 *
 * 1. **The base plane changes.** On an exact 45° plane the base-axis choice is a tie, and a tie is decided by
 *    the last bit of the normal. Six-place points resolve it differently from the source's ten-place ones in
 *    18 faces of one corpus map and 35 of another, and the same five numbers then project the texture through a
 *    different world plane: hundreds of texels, measured against `qbsp`'s own texinfo.
 * 2. **A small scale loses digits.** `0.0625004768` becomes `0.0625`: a relative change of 7.6e-6, multiplied by
 *    a world coordinate of three thousand units and divided by the scale, is an eighth of a texel.
 *
 * Neither is visible in the numbers, and neither is "SER-7's permitted precision" — APMap §14 promises the
 * source's meaning, and a texture that compiles somewhere else is a different meaning. So the encoder asks this
 * module, face by face, which five numbers mean what the source meant ON THE SURFACE THE DOCUMENT NOW HOLDS:
 *
 * - `exact` / `literal` — the SER-7 spelling of the source's own numbers. Kept whenever it reproduces the source
 *   within {@link CLASSIC_FIDELITY_BOUND_TEXELS} at every vertex of the emitted face, which is nearly always: the
 *   source's numbers stay the numbers a user reads in TEXTURE UV.
 * - `rebased` — otherwise, the source's mapping restricted to the emitted plane, re-expressed on the base plane
 *   the emitted points select. That is possible only when the restricted mapping is a rotation and two scales on
 *   that plane — its two texture gradients orthogonal there — and is used only when the rounded result is within
 *   the bound. A rebased face's numbers differ from the source's; its texels do not.
 * - `unrepresentable` — neither: the source's base plane and the emitted one see the face through a SHEAR
 *   (a two-axis tie with a non-zero third component), which no rotation and two scales can express. The literal
 *   spelling is kept, and the face is reported with its error, so the caller can name it. Nothing is invented.
 *
 * Error is measured at the emitted polygon's vertices: the difference of two affine maps is affine, so its
 * largest magnitude over a convex face is at a corner, and the same corners give the minimax shift.
 *
 * This module never touches geometry: points are rounded exactly as before, faces are never moved into another
 * bucket, and Valve 220 is never produced. Only the five Classic numbers are chosen.
 */
import { classicAxes, planeNormal, rotateProjectionAxes, type ClassicReader, type Vec3 } from '../geometry/projection.ts';

/** The fidelity a Classic face must keep through import, in texels: a hundredth of a texel, anywhere on the face. */
export const CLASSIC_FIDELITY_BOUND_TEXELS = 0.01;

export type ClassicNumbers = { shift: [number, number]; rotation: number; scale: [number, number] };
export type ClassicFidelityOutcome = 'exact' | 'literal' | 'rebased' | 'unrepresentable' | 'no_surface';

export type ClassicFidelity = {
  outcome: ClassicFidelityOutcome;
  /** The five numbers to store, every one already SER-7. */
  numbers: ClassicNumbers;
  /** Largest S/T difference from the source mapping at the emitted face's corners, in texels, for `numbers`. */
  errorTexels: number;
  /** The same for the literal spelling, so a report can say what was avoided. */
  literalErrorTexels: number;
  /** `qbsp`'s base-plane row (0 floor/ceiling, 1 X walls, 2 Y walls) for the source points and the emitted ones. */
  sourceAxis: 0 | 1 | 2;
  emittedAxis: 0 | 1 | 2;
  /** Cosine between the two re-expressed gradients on the emitted plane; 0 when a rotation and two scales fit. */
  shear: number;
};

/** One S/T affine map over world space: `s = dot(P, s.xyz) + s.w`, in texels. */
export type ClassicTexinfo = readonly [readonly [number, number, number, number], readonly [number, number, number, number]];

const dot3 = (a: readonly number[], b: readonly number[]) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];

/** Which of the three base planes a normal selects, by `reader`'s rule. */
export function classicAxisRow(normal: readonly number[], reader: ClassicReader): 0 | 1 | 2 {
  const [u, v] = classicAxes(normal, reader);
  return v[1] !== 0 ? 0 : u[1] !== 0 ? 1 : 2;
}

/**
 * The texinfo a compiler builds for a Classic face (`qbsp` `set_texinfo`): base axes turned by `rotation`, each
 * divided by its scale, the shift added. A zero scale reads as 1, exactly as `qbsp` reads it ("interpret zero
 * scale as no scaling"); the Quake II import refuses one before it gets here, other doors keep it as written.
 */
export function classicTexinfo(points: readonly (readonly number[])[], numbers: ClassicNumbers, reader: ClassicReader): ClassicTexinfo {
  const [u, v] = rotateProjectionAxes(...classicAxes(planeNormal(points), reader), numbers.rotation);
  const su = numbers.scale[0] || 1, sv = numbers.scale[1] || 1;
  return [[u[0] / su, u[1] / su, u[2] / su, numbers.shift[0]], [v[0] / sv, v[1] / sv, v[2] / sv, numbers.shift[1]]];
}

const at = (texinfo: ClassicTexinfo, point: readonly number[]): [number, number] =>
  [dot3(texinfo[0], point) + texinfo[0][3], dot3(texinfo[1], point) + texinfo[1][3]];

function maxError(source: ClassicTexinfo, candidate: ClassicTexinfo, corners: readonly (readonly number[])[]): number {
  let worst = 0;
  for (const corner of corners) {
    const a = at(source, corner), b = at(candidate, corner);
    worst = Math.max(worst, Math.abs(a[0] - b[0]), Math.abs(a[1] - b[1]));
  }
  return worst;
}

/** The base plane's two in-plane world axes (indices) and the dropped one, for a `classicAxes` pair. */
function planeIndices(u: Vec3, v: Vec3): { sv: number; tv: number; d: number; su: number; sgv: number } {
  const sv = u[0] ? 0 : u[1] ? 1 : 2, tv = v[0] ? 0 : v[1] ? 1 : 2;
  return { sv, tv, d: 3 - sv - tv, su: u[sv], sgv: v[tv] };
}

/**
 * The five numbers for the emitted face, on its own base plane, that reproduce `source` on the emitted plane —
 * or the best-fitting ones and their shear when that is impossible. Unrounded; the caller rounds.
 *
 * On the emitted plane `n·P = dist` the dropped world coordinate is an affine function of the two the base
 * plane keeps, so each source covector restricts to a gradient `g` over those two and a constant. A Classic
 * face on that base plane has gradients `σu/sx·(cos θ, sin θ)` and `σv/sy·(−sin θ, cos θ)`: orthogonal, which is
 * the representability condition, and θ, sx and sy follow from `g_s`; the constants are the shifts.
 */
function reexpress(source: ClassicTexinfo, emittedPoints: readonly (readonly number[])[], reader: ClassicReader,
  scaleHint: readonly [number, number], rotationHint: number): { candidates: ClassicNumbers[]; shear: number } {
  const n = planeNormal(emittedPoints), dist = dot3(n, emittedPoints[0]);
  const { sv, tv, d, su, sgv } = planeIndices(...classicAxes(n, reader));
  const restrict = (row: ClassicTexinfo[number]) => ({
    g: [row[sv] - row[d] * n[sv] / n[d], row[tv] - row[d] * n[tv] / n[d]] as const,
    c: row[3] + row[d] * dist / n[d],
  });
  const s = restrict(source[0]), t = restrict(source[1]);
  const length = Math.hypot(s.g[0], s.g[1]);
  const cos = s.g[0] / length, sin = s.g[1] / length;
  const shear = (cos * t.g[0] + sin * t.g[1]) / (Math.hypot(t.g[0], t.g[1]) || 1);
  const ks = su * length, kt = sgv * (-sin * t.g[0] + cos * t.g[1]);
  const theta = Math.atan2(sin, cos) * 180 / Math.PI;
  const candidates = [[theta, ks, kt], [theta + 180, -ks, -kt]].map(([angle, a, b]) => ({
    shift: [s.c, t.c] as [number, number], rotation: normaliseDegrees(angle), scale: [1 / a, 1 / b] as [number, number],
  }));
  // The spelling closest to the source's own, decided by preference and never by a last-bit difference in error:
  // the same signs of scale first, then the rotation nearer the source's, then atan2's own angle (`sort` is stable).
  const sameSigns = (c: ClassicNumbers) => Number(Math.sign(c.scale[0]) === Math.sign(scaleHint[0])) + Number(Math.sign(c.scale[1]) === Math.sign(scaleHint[1]));
  const turn = (c: ClassicNumbers) => Math.abs(normaliseDegrees(c.rotation - rotationHint));
  candidates.sort((a, b) => sameSigns(b) - sameSigns(a) || turn(a) - turn(b));
  return { candidates, shear };
}

/** An angle in (−180, 180], so a rebased rotation reads like an authored one. */
function normaliseDegrees(angle: number): number {
  let value = angle % 360;
  if (value > 180) value -= 360;
  if (value <= -180) value += 360;
  return value;
}

/** Move both shifts by the midpoint of their residual over the corners: the minimax shift for a fixed gradient. */
function centred(source: ClassicTexinfo, emittedPoints: readonly (readonly number[])[], numbers: ClassicNumbers, reader: ClassicReader,
  corners: readonly (readonly number[])[], round: (value: number) => number): ClassicNumbers {
  const candidate = classicTexinfo(emittedPoints, numbers, reader);
  const shift = [0, 1].map((axis) => {
    let low = Infinity, high = -Infinity;
    for (const corner of corners) {
      const residual = at(source, corner)[axis] - at(candidate, corner)[axis];
      low = Math.min(low, residual); high = Math.max(high, residual);
    }
    return round(numbers.shift[axis] + (low + high) / 2);
  }) as [number, number];
  return { ...numbers, shift };
}

export type ClassicFidelityInput = {
  sourcePoints: readonly (readonly number[])[];
  source: ClassicNumbers;
  emittedPoints: readonly (readonly number[])[];
  /** The emitted face's polygon, or `null` when the emitted brush gives this face no surface. Asked for only when needed. */
  corners: () => readonly (readonly number[])[] | null;
  /** SER-7 for one number. */
  round: (value: number) => number;
  reader: ClassicReader;
};

/** The five numbers to store for one Classic face, and how faithful they are. See the module comment. */
export function faithfulClassicNumbers(input: ClassicFidelityInput): ClassicFidelity {
  const { sourcePoints, source, emittedPoints, round, reader } = input;
  const literal: ClassicNumbers = { shift: [round(source.shift[0]), round(source.shift[1])], rotation: round(source.rotation),
                                    scale: [round(source.scale[0]), round(source.scale[1])] };
  const sourceAxis = classicAxisRow(planeNormal(sourcePoints), reader), emittedAxis = classicAxisRow(planeNormal(emittedPoints), reader);
  const unchanged = literal.rotation === source.rotation && literal.scale[0] === source.scale[0] && literal.scale[1] === source.scale[1];
  // Same base plane, same rotation and scales: the texture moves by the shift's rounding alone, at most 5e-7 texel,
  // on every point of every face. No polygon is needed to know that.
  if (sourceAxis === emittedAxis && unchanged) {
    const exact = literal.shift[0] === source.shift[0] && literal.shift[1] === source.shift[1];
    const error = Math.max(Math.abs(literal.shift[0] - source.shift[0]), Math.abs(literal.shift[1] - source.shift[1]));
    return { outcome: exact ? 'exact' : 'literal', numbers: literal, errorTexels: error, literalErrorTexels: error, sourceAxis, emittedAxis, shear: 0 };
  }
  const polygon = input.corners();
  if (!polygon || polygon.length < 3) {
    return { outcome: 'no_surface', numbers: literal, errorTexels: 0, literalErrorTexels: 0, sourceAxis, emittedAxis, shear: 0 };
  }
  const sourceTexinfo = classicTexinfo(sourcePoints, source, reader);
  const corners = polygon;
  const literalError = maxError(sourceTexinfo, classicTexinfo(emittedPoints, literal, reader), corners);
  if (literalError <= CLASSIC_FIDELITY_BOUND_TEXELS) {
    return { outcome: 'literal', numbers: literal, errorTexels: literalError, literalErrorTexels: literalError, sourceAxis, emittedAxis, shear: 0 };
  }
  const { candidates, shear } = reexpress(sourceTexinfo, emittedPoints, reader, source.scale, source.rotation);
  // The first candidate, in preference order, that is faithful. Two spellings of one mapping differ in error only in
  // their last bits; choosing by that would let AUE (`classicfidelity.go`) write other bytes for the same face.
  let best: { numbers: ClassicNumbers; error: number } | null = null;
  for (const candidate of candidates) {
    if (best && best.error <= CLASSIC_FIDELITY_BOUND_TEXELS) break;
    const rounded: ClassicNumbers = { shift: [round(candidate.shift[0]), round(candidate.shift[1])], rotation: round(candidate.rotation),
                                      scale: [round(candidate.scale[0]), round(candidate.scale[1])] };
    if (!rounded.scale[0] || !rounded.scale[1] || ![...rounded.shift, rounded.rotation, ...rounded.scale].every(Number.isFinite)) continue;
    const numbers = centred(sourceTexinfo, emittedPoints, rounded, reader, corners, round);
    const error = maxError(sourceTexinfo, classicTexinfo(emittedPoints, numbers, reader), corners);
    if (!best || error < best.error) best = { numbers, error };
  }
  // Rebased only when the ROUNDED numbers are faithful. They are measured on the emitted points, which are the
  // points every later read sees, and they are already SER-7, so the next save rounds nothing and rebases nothing.
  if (best && best.error <= CLASSIC_FIDELITY_BOUND_TEXELS) {
    return { outcome: 'rebased', numbers: best.numbers, errorTexels: best.error, literalErrorTexels: literalError, sourceAxis, emittedAxis, shear };
  }
  return { outcome: 'unrepresentable', numbers: literal, errorTexels: literalError, literalErrorTexels: literalError, sourceAxis, emittedAxis, shear };
}
