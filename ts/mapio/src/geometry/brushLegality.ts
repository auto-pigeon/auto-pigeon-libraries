/**
 * **Is this brush one a Quake compiler will build?** — the one canonical legality authority
 * (`AUP 140E`).
 *
 * ### Why it exists, given that every tool already crossed something
 *
 * Before this file the answer was spread across three places that each knew part of it and none of
 * which knew the whole: {@link checkBrushConvexity} for the solid, {@link brushWindingReport} for
 * which way it faces, and {@link canonicalApmapNumber} for whether the numbers survive being
 * written down. Every deformation tool crossed the first, exactly one call site in the package
 * crossed the second (`createApmapBrush`), and nothing at all checked a **texture axis** for
 * finiteness — a `NaN` in a Valve 220 `u_axis` writes `nan` into a `.map` and `qbsp` reads a plane
 * it cannot use.
 *
 * `AUP 140C` measured what that costs: 1 915 of 1 915 product brushes wound inward, discovered by an
 * external compiler rather than by the product, because *"there is no place in the product where the
 * Quake convention is asserted"*. `AUP 140D` fixed the convention at the three sites that chose it.
 * This is the crossing that would have caught it, and that catches the next one.
 *
 * ### The check is FREE, and that is the design
 *
 * `checkBrushConvexity` already runs `reconstructBrush` and already returns the `orientationSign`
 * that reconstruction settled on. For a brush that **passes** convexity, that sign is the whole
 * winding answer:
 *
 * ```text
 * reconstructBrush keeps  sign · (planeNormal·p − d) ≥ 0   so outward = −sign · planeNormal
 * qbsp's normal is        (p0−p1)×(p2−p1) = −planeNormal
 * the two agree  ⟺  sign = +1
 * ```
 *
 * So the winding costs one comparison rather than a second reconstruction — which is what makes it
 * affordable at the four thousand brushes a SURFACE DEFORM emits. {@link brushWindingReport} is
 * still used, but only to *name the faces* when something is already wrong, where the cost is paid
 * once per fault rather than once per brush.
 *
 * ### What is a REFUSAL and what is a RISK, and why the difference is measured rather than chosen
 *
 * A finding whose severity is `illegal` is one a Quake 1 compiler cannot build: it discards the
 * brush and says `Couldn't create brush faces`, or it reads a coordinate that is not a number. A
 * finding whose severity is `risk` is one worth knowing and **not** grounds for refusing anything —
 * and the list is short because it was measured rather than imagined. `AUP 140C`'s matrix compiled
 * TEXT's glyph brushes at a thinnest span of **0.5575** units and Sculpt's punch-through shoulders at
 * **0.8952**, through `qbsp` v0.18.1, with zero warnings. A minimum-thickness *refusal* would
 * therefore have deleted two shipped tools' ordinary output to enforce a number nothing measured.
 *
 * ### What is NOT here
 *
 * **No repair.** Nothing in this module rewrites a brush, and nothing calls it on the way past a
 * decode, a save or `map/writer.ts`. A document's bytes are its author's — `AUP 120J`'s rule, and
 * `quakeWinding.ts` says the same of itself. {@link apmapLegalityReport} exists so that an export
 * can *identify* an invalid stored brush; identifying is the whole of what it does.
 *
 * **No new tolerance.** Every number this module compares against is imported from the module that
 * already owned it — `CONVEXITY_TOLERANCE`, `COINCIDENT_NORMAL`, `COINCIDENT_OFFSET`,
 * `MAX_CONVEXITY_FACES`. The one number defined here is {@link THIN_BRUSH_SPAN}, and it is a
 * *reporting* bar rather than a tolerance: nothing branches on it except which severity a finding
 * carries.
 */
import { canonicalApmapNumber } from '../apmap/canonical.ts';
import { parsedBrushFromApmap } from '../apmap/document.ts';
import { isApmapBrush, type ApmapBrushItem, type ApmapDocument, type ApmapFace } from '../apmap/types.ts';
import type { ParsedBrush } from '../map/types.ts';
import { checkBrushConvexity, enclosedVolume, MAX_CONVEXITY_FACES,
         type BrushConvexity, type BrushConvexityOptions } from './convexity.ts';
import { MAX_FACE_TEXTURE_SCALE, MIN_FACE_TEXTURE_SCALE } from './tolerances.ts';
import { brushWindingReport } from './quakeWinding.ts';
import { reconstructBrush } from './reconstructBrush.ts';
import { COINCIDENT_NORMAL, COINCIDENT_OFFSET } from './tolerances.ts';

/**
 * The span below which a brush is *reported* as thin — one Quake unit, the smallest feature a
 * mapper works in.
 *
 * It is not a refusal and must not become one. `AUP 140C` compiled TEXT at 0.5575 and Sculpt's
 * punch-through at 0.8952 through `ericw-tools` v0.18.1 with zero warnings, so a brush under this
 * bar is legal geometry that somebody may still want to know about — a wall they cannot walk into,
 * a sliver a lighting pass will alias. `AUP 118`'s *refused, never clamped* applies to a **request**;
 * this is a measurement of a result.
 */
export const THIN_BRUSH_SPAN = 1;

export type BrushLegalityCode =
  /** A plane point, a texture shift/scale/rotation or a Valve 220 axis component is `NaN` or `±∞`. */
  | 'non_finite'
  /** A coordinate the canonical APMap encoder refuses, so this brush cannot be written down at all. */
  | 'unserializable'
  /** `checkBrushConvexity` said no. The convexity reason travels in `detail`. */
  | 'not_convex'
  /** The declared planes face INTO the solid, so `qbsp` intersects its complement and keeps nothing. */
  | 'winding_inward'
  /** Some faces face out and some face in: the planes do not describe one solid at all. */
  | 'winding_mixed'
  /** No interior point could be found to measure the winding against. */
  | 'winding_unreadable'
  /** Two faces declare the same plane facing the same way; one of them bounds nothing. */
  | 'duplicate_plane'
  /** More faces than {@link MAX_CONVEXITY_FACES}; the solid was not enumerated. A risk, not a verdict. */
  | 'unchecked_face_count'
  /** The solid is thinner than {@link THIN_BRUSH_SPAN} somewhere. Always a risk. */
  | 'thin'
  /** A face names no texture, or one a `.map` cannot spell. `qbsp` reads the name as a token. */
  | 'texture_unnamed'
  /** A texture scale is zero, or outside `MIN_FACE_TEXTURE_SCALE … MAX_FACE_TEXTURE_SCALE`. */
  | 'texture_scale_degenerate'
  /** A Valve 220 face's two recorded axes are parallel or zero: the projection has no inverse. */
  | 'texture_basis_degenerate';

/**
 * **The face-projection half of the contract** — `AUP 140G`.
 *
 * Everything a `.map` writes about a face's *texture*, as opposed to its plane: a name a compiler
 * can read as a token, a scale that is not zero, and — in Valve 220, which records its own axes — a
 * basis that actually spans the surface. Finiteness is not here: `faceNumbers` already covers every
 * one of these numbers and covering them twice would report one fault as two.
 *
 * Separate from `brushLegality`'s body and exported, because two callers need exactly this and
 * neither has a brush to hand: `applyFaceUvEdit` crosses the faces it is about to rewrite
 * (`AUP 137`'s tool has no Apply, so the commit door is the only door there is), and a future
 * per-face diagnostic will want the same answer without reconstructing a solid.
 *
 * ### Three rules
 *
 * 1. **A negative scale is legal.** It is how a mapper mirrors a texture, `faceUv.ts` keeps it
 *    rather than refusing it, and Quake draws it. The bound is on the **magnitude**, which is what
 *    makes "non-zero, above epsilon" a statement about the number rather than about its sign.
 * 2. **Handedness is measured, never refused.** `handedness` is the sign of
 *    `(u × v) · normal` for a recorded Valve basis — `+1`, `−1` or `0` — and a mirrored face is a
 *    real thing a mapper asked for. Only `0` is a fault, and it is the same fault as a degenerate
 *    basis, reported once.
 * 3. **The scale bounds are `faceUv.ts`'s**, imported rather than restated. A second epsilon here
 *    would be a projection this module called legal and that tool refused to produce.
 */
export type FaceProjectionCode = 'texture_unnamed' | 'texture_scale_degenerate' | 'texture_basis_degenerate';

export type FaceProjectionFinding = { code: FaceProjectionCode; detail: string };

export type FaceProjectionLegality = {
  ok: boolean;
  findings: FaceProjectionFinding[];
  /**
   * `+1`, `−1` or `0` for a Valve 220 face, measured against the face's own normal when one is
   * supplied; `0` when it cannot be measured. Classic faces derive their basis from the normal and
   * always answer `+1`, so the value is only ever interesting where the file recorded a basis.
   */
  handedness: 1 | -1 | 0;
};

/**
 * How short a recorded Valve 220 axis, or their cross product, may be before the basis is
 * degenerate.
 *
 * Not a new tolerance in spirit: an axis is a direction and a length of `1e-9` is a direction the
 * float32 UV buffer cannot represent at any texel. Stated here because `faceUv.ts` bounds the
 * *scale* and nothing bounded the *axes*, which is the gap `AUP 140E`'s finiteness scan leaves —
 * `[0, 0, 0, 0]` is perfectly finite and is not a basis.
 */
export const MIN_TEXTURE_AXIS_LENGTH = 1e-9;

/** Whether a texture name is one a `.map` can write and a compiler can read back as one token. */
const NAMEABLE = /^[^\s"]+$/;

export function faceProjectionLegality(face: { texture?: string; projection: ApmapFace['projection'] },
                                       normal?: readonly number[]): FaceProjectionLegality {
  const findings: FaceProjectionFinding[] = [];
  const projection = face.projection;
  const name = typeof face.texture === 'string' ? face.texture : '';
  if (!NAMEABLE.test(name))
    findings.push({ code: 'texture_unnamed',
                    detail: name ? `the texture name ${JSON.stringify(name)} holds whitespace or a quote, which a .map cannot spell as one token`
                                 : 'the face names no texture' });

  // A Quake III brushDef face (`Q3_002`) has no scale: its matrix IS the scale, rotation and shear, and
  // the one way it can fail to be a projection is SEM-Q-3's — a zero determinant collapses the texture
  // to a line. That is the same fault as a degenerate Valve basis and is reported as one.
  if (projection.mode === 'brush_primitives') {
    const [[a, b], [d, e]] = projection.matrix;
    const determinant = a * e - b * d;
    if (!(Math.abs(determinant) > 0) || !Number.isFinite(determinant))
      findings.push({ code: 'texture_basis_degenerate', detail: `the texture matrix has determinant ${determinant}, so it maps the face onto a line` });
    return { ok: !findings.length, findings, handedness: determinant > 0 ? 1 : determinant < 0 ? -1 : 0 };
  }
  for (const [axis, value] of (['U', 'V'] as const).map((label, index) => [label, projection.scale?.[index]] as const)) {
    const size = Math.abs(Number(value));
    if (!Number.isFinite(size) || size < MIN_FACE_TEXTURE_SCALE || size > MAX_FACE_TEXTURE_SCALE)
      findings.push({ code: 'texture_scale_degenerate',
                      detail: `scale ${axis} is ${value}, outside ${MIN_FACE_TEXTURE_SCALE} … ${MAX_FACE_TEXTURE_SCALE} in magnitude` });
  }

  let handedness: 1 | -1 | 0 = 0;
  if (projection.mode === 'valve220') {
    const u = projection.u_axis, v = projection.v_axis;
    const lengthU = Math.hypot(u[0], u[1], u[2]), lengthV = Math.hypot(v[0], v[1], v[2]);
    const perpendicular = cross(u, v);
    const area = Math.hypot(perpendicular[0], perpendicular[1], perpendicular[2]);
    if (!(lengthU > MIN_TEXTURE_AXIS_LENGTH) || !(lengthV > MIN_TEXTURE_AXIS_LENGTH) || !(area > MIN_TEXTURE_AXIS_LENGTH))
      findings.push({ code: 'texture_basis_degenerate',
                      detail: `the recorded axes span no surface: |u| ${lengthU.toFixed(6)}, |v| ${lengthV.toFixed(6)}, |u x v| ${area.toFixed(6)}` });
    else if (normal && normal.length === 3) {
      const along = dot(perpendicular, normal);
      handedness = along > 0 ? 1 : along < 0 ? -1 : 0;
    }
  } else {
    handedness = 1;
  }
  return { ok: !findings.length, findings, handedness };
}

export type BrushLegalitySeverity = 'illegal' | 'risk';

export type BrushLegalityFinding = {
  code: BrushLegalityCode;
  severity: BrushLegalitySeverity;
  /** One sentence naming what was found. Diagnostic text, never a user-facing string. */
  detail: string;
  /** The face indices responsible, where they are determinable. Empty when they are not. */
  faces: number[];
};

export type BrushLegality = {
  /** True exactly when no finding has severity `illegal`. Risks do not make a brush illegal. */
  ok: boolean;
  findings: BrushLegalityFinding[];
  /** `illegal` codes only, in the order found — what a caller reports without re-filtering. */
  codes: BrushLegalityCode[];
  /** The convexity result, so a caller that needs the volume or the sign does not run it twice. */
  convexity: BrushConvexity;
  /** `'outward'` is the only legal answer. Derived from the convexity sign where it is trustworthy. */
  winding: 'outward' | 'inward' | 'mixed' | 'unreadable';
  /** Enclosed volume in cubic Quake units, as `checkBrushConvexity` measured it. */
  volume: number;
  /** The thinnest span across the solid, or `Infinity` when it could not be measured. */
  thinnestSpan: number;
};

/**
 * What `convexity` reads as when the brush could not be parsed at all.
 *
 * Not "it passed" and not "it failed": the finding that stopped the parse is already recorded, and
 * a second manufactured convexity verdict would be that one fault counted twice.
 */
const UNCHECKED_CONVEXITY: BrushConvexity = { ok: false, faces: [], vertexCount: 0, volume: 0, orientationSign: 0 };

const dot = (a: readonly number[], b: readonly number[]) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
const sub = (a: readonly number[], b: readonly number[]) => [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
const cross = (a: readonly number[], b: readonly number[]) =>
  [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];

/** Every number a face writes into a `.map`, plane points and texture alike. Order is not read. */
function faceNumbers(face: ApmapFace): number[] {
  const projection = face.projection;
  if (projection.mode === 'brush_primitives') return [...face.plane.points.flat(), ...projection.matrix.flat()];
  return [
    ...face.plane.points.flat(),
    ...(projection.mode === 'valve220'
      ? [...projection.u_axis, ...projection.v_axis]
      : projection.shift),
    projection.rotation, ...projection.scale,
  ];
}

/**
 * How thin this solid gets, measured as the smallest distance from any face plane to the farthest
 * corner on its inward side.
 *
 * The same reading `checkBrushConvexity` takes for its half-space test, kept rather than recomputed:
 * for each face, the deepest the solid reaches behind it. The minimum over faces is the thinnest
 * direction, because a convex solid's width along a face's normal is exactly that depth.
 */
function thinnestSpanOf(brush: ParsedBrush, orientationSign: number, built: Reconstruction): number {
  const corners = built.faces.flatMap((face) => face.polygon);
  if (corners.length < 4) return Infinity;
  let thinnest = Infinity;
  for (const face of brush.faces) {
    const raw = cross(sub(face.points[1], face.points[0]), sub(face.points[2], face.points[0]));
    const length = Math.hypot(raw[0], raw[1], raw[2]);
    if (!length) continue;
    const n = raw.map((value) => (-orientationSign * value) / length);
    const d = dot(n, face.points[0]);
    let deepest = 0;
    for (const corner of corners) deepest = Math.max(deepest, d - dot(n, corner));
    thinnest = Math.min(thinnest, deepest);
  }
  return thinnest;
}

/** What `reconstructBrush` answers. Named so a reuse can be typed without importing its shape. */
export type Reconstruction = ReturnType<typeof reconstructBrush>;

export type BrushLegalityOptions = BrushConvexityOptions & {
  /** The label the canonical encoder puts in its messages. Diagnostic only. */
  context?: string;
  /**
   * **Work this brush's own caller has already done**, so a crossing on a hot path does not pay for
   * the same reconstruction twice (`AUP 140E`).
   *
   * Every member must be derived from **this brush's plane points** — `parsed` its `ParsedBrush`
   * view, `built` `reconstructBrush(parsed)`, `convexity` `checkBrushConvexity(parsed, options)`.
   * A projection may differ (the axial engine transports textures after it reconstructs, and a
   * projection moves no plane); a plane point may not.
   *
   * The contract is trust, so it is **checked rather than trusted**: `brushLegalityReuse.test.ts`
   * runs every cell of a real bend, a squeeze and an axial deform through both the reused and the
   * from-scratch path and demands identical findings. That is `AUP 132`'s rule — the unscoped form
   * is the oracle — applied to the one place where a caller can make this module lie.
   */
  reuse?: { parsed: ParsedBrush; built: Reconstruction; convexity: BrushConvexity };
};

/**
 * **The whole contract, for one brush.**
 *
 * Finite coordinates and texture axes; non-collinear plane points and non-zero normals; no
 * duplicate same-facing planes; consistent plane orientation; a bounded, non-empty convex
 * half-space intersection; positive volume; and the thinnest span measured and reported.
 *
 * Deterministic and pure: no clock, no counter, no cache. Two clients checking the same brush get
 * the same findings in the same order, which is what lets a preview and an Apply compare.
 */
export function brushLegality(brush: ApmapBrushItem, options: BrushLegalityOptions = {}): BrushLegality {
  const context = options.context ?? 'brush-legality';
  const findings: BrushLegalityFinding[] = [];
  const add = (code: BrushLegalityCode, severity: BrushLegalitySeverity, detail: string, faces: number[] = []) =>
    findings.push({ code, severity, detail, faces });

  // 1. Finiteness, first, because every measurement below reads these numbers. A `NaN` plane point
  //    makes a normal `NaN`, which makes every dot product `NaN`, which makes every comparison
  //    false — so a check that ran in the other order would report a *convex* brush made of nothing.
  const nonFinite: number[] = [];
  for (const [index, face] of brush.faces.entries())
    if (faceNumbers(face).some((value) => !Number.isFinite(value))) nonFinite.push(index);
  if (nonFinite.length)
    add('non_finite', 'illegal',
        `face${nonFinite.length > 1 ? 's' : ''} ${nonFinite.join(', ')} declare${nonFinite.length > 1 ? '' : 's'} a coordinate or texture axis that is not a finite number`,
        nonFinite);

  // 2. Writability. The canonical encoder is what a save and a `.map` export both go through, so a
  //    brush it refuses is one that cannot reach a compiler however legal its geometry is.
  if (!nonFinite.length)
    try { for (const face of brush.faces) for (const value of faceNumbers(face)) canonicalApmapNumber(value, context, 'value'); }
    catch (cause) { add('unserializable', 'illegal', (cause as Error)?.message ?? String(cause)); }

  // 2b. The face's TEXTURE, as opposed to its plane (`AUP 140G`). Reported per face and per code, so
  //     a forty-face brush with one nameless face names that face. `faceProjectionLegality` owns the
  //     rules and the bounds; this is where a brush inherits them.
  {
    const byCode = new Map<FaceProjectionCode, { faces: number[]; detail: string }>();
    for (const [index, face] of brush.faces.entries()) {
      for (const finding of faceProjectionLegality(face).findings) {
        const held = byCode.get(finding.code);
        if (held) held.faces.push(index);
        else byCode.set(finding.code, { faces: [index], detail: finding.detail });
      }
    }
    for (const [code, found] of byCode)
      add(code, 'illegal',
          `face${found.faces.length > 1 ? 's' : ''} ${found.faces.join(', ')}: ${found.detail}`, found.faces);
  }

  // The APMap decoder REFUSES a non-finite member outright, so a brush carrying one cannot be
  // parsed at all — which is why the finiteness scan runs first and why this parse is guarded. A
  // caller handed a document from a worker, a peer or a file gets an answer either way; throwing
  // here would make the one function that exists to report a fault fail on the faults it reports.
  let parsed: ParsedBrush | undefined;
  if (!nonFinite.length)
    try { parsed = options.reuse?.parsed ?? parsedBrushFromApmap(brush, 0, context); }
    catch (cause) { add('unserializable', 'illegal', (cause as Error)?.message ?? String(cause)); }

  const convexity = parsed ? options.reuse?.convexity ?? checkBrushConvexity(parsed, options) : UNCHECKED_CONVEXITY;
  if (parsed && !convexity.ok)
    add(convexity.reason === 'too_many_faces' ? 'unchecked_face_count' : 'not_convex',
        convexity.reason === 'too_many_faces' ? 'risk' : 'illegal',
        convexity.detail ?? convexity.reason ?? 'not a convex solid', convexity.faces);

  // 3. Duplicate same-facing planes. `checkBrushConvexity`'s completeness rule catches a plane that
  //    bounds nothing, but two *identical* planes both carry the same facet and both pass it — so
  //    this is its own reading, at `solidFromPlanes`' own two tolerances rather than a third.
  const duplicates: number[] = [];
  const planes = brush.faces.map((face) => {
    const raw = cross(sub(face.plane.points[1], face.plane.points[0]), sub(face.plane.points[2], face.plane.points[0]));
    const length = Math.hypot(raw[0], raw[1], raw[2]);
    return length ? { n: raw.map((value) => value / length), d: 0 } : undefined;
  }).map((plane, index) => plane ? { ...plane, d: dot(plane.n, brush.faces[index].plane.points[0]) } : undefined);
  for (let i = 0; i < planes.length; i += 1)
    for (let j = i + 1; j < planes.length; j += 1) {
      const a = planes[i], b = planes[j];
      if (!a || !b) continue;
      if (dot(a.n, b.n) >= COINCIDENT_NORMAL && Math.abs(a.d - b.d) <= COINCIDENT_OFFSET && !duplicates.includes(j))
        duplicates.push(j);
    }
  if (duplicates.length)
    add('duplicate_plane', 'illegal',
        `face${duplicates.length > 1 ? 's' : ''} ${duplicates.join(', ')} restate${duplicates.length > 1 ? '' : 's'} a plane another face already declares, facing the same way`,
        duplicates);

  // 4. The winding. Free where convexity passed — the sign IS the answer — and measured properly
  //    where it did not, because a caller looking at a broken brush wants to know which faces.
  let winding: BrushLegality['winding'];
  let offending: number[] = [];
  if (convexity.ok && parsed) {
    winding = convexity.orientationSign === 1 ? 'outward' : 'inward';
    if (winding === 'inward') offending = brush.faces.map((_, index) => index);
  } else {
    const report = brushWindingReport(brush);
    winding = report.winding === 'unreconstructable' ? 'unreadable' : report.winding;
    offending = report.faces.filter((face) => face.winding !== 'outward').map((face) => face.index);
  }
  if (winding !== 'outward')
    add(winding === 'inward' ? 'winding_inward' : winding === 'mixed' ? 'winding_mixed' : 'winding_unreadable',
        // An unreadable winding is only ever a *second* symptom: the brush already failed
        // convexity or finiteness, and reporting it as an independent fault would be one defect
        // counted twice. It is a risk so that it appears in the report without inflating the verdict.
        winding === 'unreadable' ? 'risk' : 'illegal',
        winding === 'inward'
          ? 'every declared plane faces into the solid, so a Quake compiler intersects its complement and keeps nothing'
          : winding === 'mixed'
            ? 'some faces face out of the solid and some into it, so the planes do not describe one solid'
            : 'no interior point could be found to measure the winding against',
        winding === 'unreadable' ? [] : offending);

  const built = parsed ? options.reuse?.built ?? reconstructBrush(parsed) : undefined;
  const volume = convexity.ok ? convexity.volume : built ? enclosedVolume(built.faces) : 0;
  const thinnestSpan = convexity.ok && parsed && built ? thinnestSpanOf(parsed, convexity.orientationSign, built) : Infinity;
  if (Number.isFinite(thinnestSpan) && thinnestSpan < THIN_BRUSH_SPAN)
    add('thin', 'risk', `the solid is ${thinnestSpan.toFixed(4)} units across at its thinnest, under the ${THIN_BRUSH_SPAN}-unit reporting bar`);

  const codes = findings.filter((finding) => finding.severity === 'illegal').map((finding) => finding.code);
  return { ok: !codes.length, findings, codes, convexity, winding, volume, thinnestSpan };
}

/**
 * **The emission-door contract: an operation may not INTRODUCE an illegality** — `AUP 140F`.
 *
 * `AUP 140E` put the legality crossing at the axial engine's emission door, where every brush is
 * built from scratch and *"must be legal"* is the whole rule. The generator and boolean family is
 * different in one way that matters: `array`, `csg` and `splitBrush` act on brushes a **mapper
 * authored elsewhere**, and a document saved before `AUP 140D` is inside out. §151 rule 5 already
 * refused to put the crossing in `settleSolid` for exactly that reason — *"a crossing there would
 * refuse to CSG, split or crop a legacy map"* — and putting an unconditional one at the door has
 * the same effect one function further out.
 *
 * So the contract these tools keep is stated against their own inputs: **every finding the output
 * has that its sources did not is a refusal.** For TEXT and SOLID FROM IMAGE, which have no source
 * at all, the set is empty and the rule reduces to *"the output must be legal"*, which is what the
 * prompt asks of a generator. For a carve of a legacy inside-out brush it says the fragments may be
 * inside out too — they are the same solid — while still refusing a fragment that is non-convex,
 * has no volume, restates a plane or carries a `NaN`, none of which the source had.
 *
 * The inherited codes are computed once per operation, from the source brushes, and every one is
 * reported whether or not it was inherited: identifying is never conditional on refusing
 * (`AUP 140E`'s export rule, one layer in).
 */
/** The inherited set for a tool with no source brushes — TEXT and SOLID FROM IMAGE. */
export const NO_INHERITED_ILLEGALITY: ReadonlySet<BrushLegalityCode> = new Set<BrushLegalityCode>();

export type EmissionLegality = {
  legality: BrushLegality;
  /** `illegal` codes this brush has and its sources did not. Empty means the door is open. */
  introduced: BrushLegalityCode[];
};

/**
 * Cross one emitted brush against the door.
 *
 * `inherited` is what {@link sourceLegalityCodes} answered for the operation's inputs; `options`
 * takes `AUP 140E`'s `reuse`, so a caller that has already reconstructed pays 25 µs rather than 123.
 */
export function emissionLegality(brush: ApmapBrushItem, inherited: ReadonlySet<BrushLegalityCode>,
                                 options: BrushLegalityOptions = {}): EmissionLegality {
  const legality = brushLegality(brush, options);
  return { legality, introduced: legality.codes.filter((code) => !inherited.has(code)) };
}

/**
 * The illegal codes an operation's **sources** already carry.
 *
 * A brush too malformed to parse contributes its own findings rather than throwing: the point of
 * this set is to describe what was already wrong, and a source that cannot be read at all is the
 * strongest possible statement of that.
 */
export function sourceLegalityCodes(brushes: Iterable<ApmapBrushItem>,
                                    options: BrushConvexityOptions & { context?: string } = {}):
  Set<BrushLegalityCode> {
  const codes = new Set<BrushLegalityCode>();
  for (const brush of brushes) for (const code of brushLegality(brush, options).codes) codes.add(code);
  return codes;
}

/** Shorthand: is this brush one a Quake compiler will build? */
export const brushIsLegal = (brush: ApmapBrushItem, options?: BrushConvexityOptions): boolean =>
  brushLegality(brush, options).ok;

export type ApmapLegalityFinding = BrushLegalityFinding & { entityId: string; brushId: string };

export type ApmapLegalityReport = {
  /** Every brush the document holds, whether or not it was found wanting. */
  brushes: number;
  /** Brushes with at least one `illegal` finding. */
  illegal: number;
  /** Brushes with at least one `risk` finding and no `illegal` one. */
  atRisk: number;
  /** Bounded at {@link APMAP_LEGALITY_FINDING_LIMIT}; `truncated` says when that bit. */
  findings: ApmapLegalityFinding[];
  truncated: boolean;
};

/**
 * How many findings one document report carries.
 *
 * A bound rather than a tidy number: a map saved before `AUP 140D` has *every* brush wound inward,
 * and `dm2` is 1 252 of them. A report that listed all of them would be a megabyte of JSON in an
 * export archive, and the reader's decision does not change after the first dozen.
 */
export const APMAP_LEGALITY_FINDING_LIMIT = 64;

/**
 * **Identify, never repair** — the defensive scan an export runs over a document it did not author.
 *
 * `AUP 140E`'s export clause: a `.map` that carries a brush a compiler will discard must *say so*,
 * naming the brush, rather than shipping bytes that fail two programs later with
 * `Couldn't create brush faces` and no attribution. Nothing here rewrites, drops or reorders
 * anything; `mapTextFromApmap` remains the verbatim passthrough it has always been.
 */
export function apmapLegalityReport(document: ApmapDocument, options: BrushConvexityOptions = {}): ApmapLegalityReport {
  let brushes = 0, illegal = 0, atRisk = 0, truncated = false;
  const findings: ApmapLegalityFinding[] = [];
  for (const entity of document.entities)
    for (const item of entity.content) {
      if (!isApmapBrush(item)) continue;
      brushes += 1;
      const legality = brushLegality(item, { ...options, context: 'apmap-export' });
      if (!legality.findings.length) continue;
      if (legality.ok) atRisk += 1; else illegal += 1;
      for (const finding of legality.findings) {
        if (findings.length >= APMAP_LEGALITY_FINDING_LIMIT) { truncated = true; continue; }
        findings.push({ ...finding, entityId: entity.entity_id, brushId: item.brush_id });
      }
    }
  return { brushes, illegal, atRisk, findings, truncated };
}
