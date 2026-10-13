/**
 * Quake II under APMap 1.5 (Q2_001): what 1.5 already represents, proved rather than assumed.
 *
 * No schema changed for Quake II. These tests hold the `q2-*` vectors to SEMANTICS.md §6.3a — tail
 * order, absent versus explicit zero, the signed 32-bit range, unassigned bits — and carry the
 * reference math for the projection claims: classic → Valve 220 is exact, and an oblique Valve 220
 * face has no classic form. The compiler measurements quoted in §6.3a were taken with the pinned
 * EricW builds on authored maps; nothing here runs a compiler.
 */
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import {
  INT32_MAX, INT32_MIN, compile, describeErrors, loadCurrentSchema, quake2AdmissionFaults, semanticFaults,
  vector, vectorIndex,
} from './helpers.mjs';

const validateCurrent = compile(loadCurrentSchema());
const index = vectorIndex();
const VECTORS = path.join(import.meta.dirname, '..', 'test-vectors');
const Q2_VALID = index.valid.map((entry) => entry.file).filter((file) => file.startsWith('q2-'));
const bytesOf = (kind, file) => fs.readFileSync(path.join(VECTORS, kind, file), 'utf8');
const brushesOf = (document) => document.entities.flatMap((entity) => entity.content.filter((item) => item.kind === 'brush'));
const facesOf = (document) => brushesOf(document).flatMap((brush) => brush.faces);
const face = (document, id) => facesOf(document).find((candidate) => candidate.face_id === id);

// ---------------------------------------------------------------------------------------------
// The document shape: game quake2, the projection form as dialect, nothing from Quake III
// ---------------------------------------------------------------------------------------------

test('every Quake II vector is game quake2 in a projection-form dialect, valid, and canonical', () => {
  assert.equal(Q2_VALID.length, 6);
  for (const file of Q2_VALID) {
    const document = vector('valid', file);
    assert.equal(document.game, 'quake2', file);
    assert.ok(['quake_standard', 'valve_220'].includes(document.map_dialect), `${file}: ${document.map_dialect}`);
    assert.ok(validateCurrent(document), `${file}:\n  ${describeErrors(validateCurrent.errors)}`);
    assert.deepEqual(semanticFaults(document), [], file);
    assert.deepEqual(quake2AdmissionFaults(document), [], `${file} must be buildable as Quake II`);
    const bytes = bytesOf('valid', file);
    assert.equal(`${JSON.stringify(JSON.parse(bytes), null, 2)}\n`, bytes, `${file} is not a canonical fixed point`);
    assert.ok(!/[0-9][eE][-+]?[0-9]/.test(bytes), `${file} uses exponent notation`);
  }
});

test('no Quake III member is copied into a Quake II vector', () => {
  for (const file of Q2_VALID) {
    const document = vector('valid', file);
    assert.ok(document.entities.every((entity) => entity.content.every((item) => item.kind !== 'patch')), file);
    assert.ok(facesOf(document).every((candidate) => candidate.projection.mode !== 'brush_primitives'), file);
  }
  // And the one that tries is refused for SEM-Q-4, the rule that already covers it.
  const smuggled = vector('semantic-invalid', 'q2-patch-in-quake2-document.apmap');
  assert.ok(validateCurrent(smuggled));
  assert.deepEqual(semanticFaults(smuggled).map((fault) => fault.rule), ['SEM-Q-4']);
});

// ---------------------------------------------------------------------------------------------
// §6.3a: order, absent versus zero, range
// ---------------------------------------------------------------------------------------------

test('an absent tail and an explicit [0,0,0] are different data and both survive', () => {
  const document = vector('valid', 'q2-classic-sealed-room.apmap');
  const absent = facesOf(document).filter((candidate) => !('tail' in candidate));
  assert.deepEqual(absent.map((candidate) => candidate.face_id), ['fac_q2v01floor0f5']);
  assert.ok(facesOf(document).filter((candidate) => 'tail' in candidate).length > 30);
  // A JSON round trip is the minimum any reader does; it must neither add nor drop the member.
  const again = JSON.parse(JSON.stringify(document));
  assert.ok(!('tail' in face(again, 'fac_q2v01floor0f5')));
  assert.deepEqual(face(again, 'fac_q2v01floor0f4').tail, [0, 0, 0]);
});

/** A WAL header's three fields, in the WAL's OWN order: flags, contents, value. */
const walHeaderOf = ({ flags, contents, value }) => [flags, contents, value];
/** A face tail's three fields, in the `.map` order: contents, flags, value. */
const tailFields = ([contents, flags, value]) => ({ contents, flags, value });

test('tail order is CONTENTS, SURFACE FLAGS, VALUE, and a WAL header is not', () => {
  const marker = face(vector('valid', 'q2-classic-sealed-room.apmap'), 'fac_q2v01mark0f0');
  assert.deepEqual(marker.tail, [1, 2, 300]);
  const fields = tailFields(marker.tail);
  assert.deepEqual(fields, { contents: 1, flags: 2, value: 300 });
  // Mapping by NAME gives the WAL header [2, 1, 300]; copying by position would have swapped
  // SOLID and SLICK — the transposition this test exists to make visible.
  assert.deepEqual(walHeaderOf(fields), [2, 1, 300]);
  assert.notDeepEqual(walHeaderOf(fields), marker.tail);
});

test('every Quake II tail is a signed 32-bit integer triple that survives as a JSON integer', () => {
  const document = vector('valid', 'q2-tail-bits.apmap');
  const tails = new Map(brushesOf(document).map((brush) => [brush.brush_id, brush.faces[0].tail]));
  assert.deepEqual(tails.get('brs_q2v03detail000'), [0x08000000, 0, 0]);
  assert.deepEqual(tails.get('brs_q2v03ladderclp'), [0x28830000, 0, 0]);
  assert.deepEqual(tails.get('brs_q2v03solidsign'), [0x80000001 | 0, 0, 0]);
  assert.deepEqual(tails.get('brs_q2v03int32min0'), [INT32_MIN, 0, 0]);
  assert.deepEqual(tails.get('brs_q2v03unkcont00'), [0x401, 0, 0]);
  assert.deepEqual(tails.get('brs_q2v03unkflags0'), [1, 0xC00, 0]);
  assert.deepEqual(tails.get('brs_q2v03allflags0'), [1, INT32_MAX, INT32_MAX]);
  const bytes = bytesOf('valid', 'q2-tail-bits.apmap');
  for (const literal of ['134217728', '679673856', '-2147483647', '-2147483648', '2147483647'])
    assert.ok(bytes.includes(`${literal},`) || bytes.includes(`${literal}\n`), `${literal} is not spelled as an integer`);
  for (const value of facesOf(document).flatMap((candidate) => candidate.tail ?? [])) {
    assert.ok(Number.isInteger(value) && value >= INT32_MIN && value <= INT32_MAX, `${value}`);
    // Exact in an IEEE double, so a float64 reader (AUE) and a JS reader (AUP, AUC) agree.
    assert.ok(Math.abs(value) < 2 ** 53);
  }
});

test('every face of a brush carries one tail, so the vectors never ask a compiler to mix contents', () => {
  for (const file of Q2_VALID)
    for (const brush of brushesOf(vector('valid', file))) {
      const tails = new Set(brush.faces.filter((candidate) => 'tail' in candidate).map((candidate) => JSON.stringify(candidate.tail)));
      assert.ok(tails.size <= 1, `${file} ${brush.brush_id} mixes tails ${[...tails].join(' ')}`);
    }
});

// ---------------------------------------------------------------------------------------------
// ADM-Q2-*: refused by a build, never by a reader
// ---------------------------------------------------------------------------------------------

test('every Quake II admission vector is a valid document refused by exactly its ADM-Q2 rule', () => {
  const sheet = index.quake2_admission;
  assert.ok(sheet.length >= 7);
  const onDisk = fs.readdirSync(path.join(VECTORS, 'quake2-admission')).sort();
  assert.deepEqual(sheet.map((entry) => entry.file).sort(), onDisk);
  const rules = new Set();
  for (const entry of sheet) {
    const document = vector('quake2-admission', entry.file);
    assert.ok(validateCurrent(document), `${entry.file} must pass the schema:\n  ${describeErrors(validateCurrent.errors)}`);
    assert.deepEqual(semanticFaults(document), [], `${entry.file} must pass every SEM rule`);
    const faults = quake2AdmissionFaults(document);
    assert.equal(faults.length, 1, `${entry.file}: ${JSON.stringify(faults)}`);
    assert.equal(faults[0].rule, entry.rule, entry.file);
    if (entry.face_id) assert.equal(faults[0].face_id, entry.face_id, entry.file);
    if (entry.entity_id) assert.deepEqual([faults[0].entity_id, faults[0].key], [entry.entity_id, entry.key], entry.file);
    rules.add(entry.rule);
  }
  assert.deepEqual([...rules].sort(), ['ADM-Q2-1', 'ADM-Q2-2', 'ADM-Q2-3', 'ADM-Q2-4']);
});

// ---------------------------------------------------------------------------------------------
// Projections. Reference math: id's qbsp3 / EricW `TextureAxisFromPlane` + `QuakeTextureVecs`
// for classic, and the Valve 220 axes as written (s = dot(p, u) / scale + shift).
// ---------------------------------------------------------------------------------------------

const BASE_AXIS = [
  [[0, 0, 1], [1, 0, 0], [0, -1, 0]], [[0, 0, -1], [1, 0, 0], [0, -1, 0]],
  [[1, 0, 0], [0, 1, 0], [0, 0, -1]], [[-1, 0, 0], [0, 1, 0], [0, 0, -1]],
  [[0, 1, 0], [1, 0, 0], [0, 0, -1]], [[0, -1, 0], [1, 0, 0], [0, 0, -1]],
];
const dot = (a, b) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
const sub = (a, b) => [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
const cross = (a, b) => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];
const unit = (a) => { const l = Math.hypot(...a); return a.map((x) => x / l); };
const normalOf = ([p0, p1, p2]) => unit(cross(sub(p0, p1), sub(p2, p1)));
const ser7 = (x) => (Math.round(x * 1e6) / 1e6) || 0;

function baseAxes(normal) {
  let best = 0;
  let bestDot = 0;
  BASE_AXIS.forEach(([axis], i) => { const d = dot(normal, axis); if (d > bestDot) { bestDot = d; best = i; } });
  return [BASE_AXIS[best][1], BASE_AXIS[best][2]];
}

/** Classic → the rotated, unscaled texture axes; exact sines at the right angles, as qbsp3 does. */
function rotatedAxes(normal, rotation) {
  const axes = baseAxes(normal).map((v) => [...v]);
  const exact = { 0: [0, 1], 90: [1, 0], 180: [0, -1], 270: [-1, 0], '-90': [-1, 0] };
  const [sin, cos] = exact[rotation] ?? [Math.sin(rotation / 180 * Math.PI), Math.cos(rotation / 180 * Math.PI)];
  const sv = axes[0].findIndex((x) => x !== 0);
  const tv = axes[1].findIndex((x) => x !== 0);
  for (const v of axes) [v[sv], v[tv]] = [cos * v[sv] - sin * v[tv], sin * v[sv] + cos * v[tv]];
  return { axes, sv, tv };
}

const classicUV = (points, { shift, rotation, scale }, p) => {
  const { axes } = rotatedAxes(normalOf(points), rotation);
  return [dot(p, axes[0]) / scale[0] + shift[0], dot(p, axes[1]) / scale[1] + shift[1]];
};
const valveUV = ({ u_axis: u, v_axis: v, scale }, p) =>
  [dot(p, u.slice(0, 3)) / scale[0] + u[3], dot(p, v.slice(0, 3)) / scale[1] + v[3]];

/** Classic → Valve 220: the rotated base axes, the same scale and shift. Exact by construction. */
function classicToValve(points, projection, round = (x) => x) {
  const { axes } = rotatedAxes(normalOf(points), projection.rotation);
  return { mode: 'valve220', u_axis: [...axes[0].map(round), projection.shift[0]],
           v_axis: [...axes[1].map(round), projection.shift[1]], rotation: projection.rotation, scale: projection.scale };
}

/**
 * Valve 220 → classic, or null. On the face's plane a classic projection is s = (R·e)/scale over
 * the two base-axis coordinates, so its two rows are ORTHOGONAL there; a Valve face whose rows,
 * projected the same way, are not orthogonal (shear) has no classic form at any rotation or scale.
 */
function valveToClassic(points, projection) {
  const normal = normalOf(points);
  const { sv, tv } = rotatedAxes(normal, 0);
  const [base0, base1] = baseAxes(normal);
  // Restrict each axis to the plane: express dot(p, axis) as a·p[sv] + b·p[tv] + c.
  const third = 3 - sv - tv;
  const along = (axis) => {
    const k = Math.abs(normal[third]) < 1e-12 ? 0 : axis[third] / normal[third];
    return [axis[sv] - k * normal[sv], axis[tv] - k * normal[tv]];
  };
  const [a0, a1] = along(projection.u_axis.slice(0, 3)).map((x) => x / projection.scale[0]);
  const [b0, b1] = along(projection.v_axis.slice(0, 3)).map((x) => x / projection.scale[1]);
  // In base coordinates: undo the base axes' signs on sv and tv.
  // A classic row is R(θ)·e/scale with e = ±1 the base axis's sign on its own coordinate.
  const [r0, r1] = [[a0 * base0[sv], a1 * base0[sv]], [b0 * base1[tv], b1 * base1[tv]]];
  const lengths = [Math.hypot(...r0), Math.hypot(...r1)];
  if (Math.abs(r0[0] * r1[0] + r0[1] * r1[1]) > 1e-6 * lengths[0] * lengths[1]) return null;
  // Not sheared: rows are R(θ)·e_s/scale0 and R(θ)·e_t/scale1 (scales may be negative).
  const theta = Math.atan2(r0[1], r0[0]);
  const scale0 = 1 / lengths[0];
  const scale1 = (Math.cos(theta) * r1[1] - Math.sin(theta) * r1[0]) > 0 ? 1 / lengths[1] : -1 / lengths[1];
  return { mode: 'classic', shift: [projection.u_axis[3], projection.v_axis[3]],
           rotation: theta * 180 / Math.PI, scale: [scale0, scale1] };
}

/** Four probe points on a face's plane, spread over ±span along the plane's base axes. */
function probes(points, span) {
  const normal = normalOf(points);
  const { sv, tv } = rotatedAxes(normal, 0);
  const third = 3 - sv - tv;
  const [p0] = points;
  const onPlane = (x, y) => {
    const p = [0, 0, 0]; p[sv] = x; p[tv] = y;
    p[third] = p0[third] - (normal[sv] * (x - p0[sv]) + normal[tv] * (y - p0[tv])) / normal[third];
    return p;
  };
  return [onPlane(p0[sv], p0[tv]), onPlane(span, -span), onPlane(-span, span), onPlane(span * 0.5, span)];
}

const maxDelta = (a, b) => Math.max(Math.abs(a[0] - b[0]), Math.abs(a[1] - b[1]));
const classicFaces = () => brushesOf(vector('valid', 'q2-classic-projections.apmap'))
  .find((brush) => brush.brush_id === 'brs_q2v04class0').faces;

test('the classic vector covers 0, 90 and 15 degrees, signed and non-uniform scales, fractional shifts', () => {
  const projections = classicFaces().map((candidate) => candidate.projection);
  assert.deepEqual([...new Set(projections.map((p) => p.rotation))].sort((a, b) => a - b), [0, 15, 90]);
  assert.ok(projections.some((p) => p.scale[0] < 0) && projections.some((p) => p.scale[1] < 0));
  assert.ok(projections.some((p) => p.scale[0] !== p.scale[1]));
  assert.ok(projections.some((p) => !Number.isInteger(p.shift[0])));
});

test('classic -> Valve 220 keeps every world-space texture coordinate (|delta| < 1e-9; < 0.01 after SER-7)', () => {
  for (const candidate of classicFaces()) {
    const { points } = candidate.plane;
    const exact = classicToValve(points, candidate.projection);
    const rounded = classicToValve(points, candidate.projection, ser7);
    for (const p of probes(points, 4096)) {
      const want = classicUV(points, candidate.projection, p);
      assert.ok(maxDelta(valveUV(exact, p), want) < 1e-9, `${candidate.face_id} at ${p}`);
      assert.ok(maxDelta(valveUV(rounded, p), want) < 0.01, `${candidate.face_id} at ${p} after SER-7`);
    }
  }
});

test('Valve 220 -> classic exists for axial faces and round-trips at the probe points', () => {
  for (const candidate of classicFaces()) {
    const { points } = candidate.plane;
    const back = valveToClassic(points, classicToValve(points, candidate.projection));
    assert.ok(back, `${candidate.face_id} should have a classic form`);
    for (const p of probes(points, 4096))
      assert.ok(maxDelta(classicUV(points, back, p), classicUV(points, candidate.projection, p)) < 1e-6, candidate.face_id);
  }
});

test('the oblique Valve 220 slope has no classic form, so a converter must refuse it', () => {
  const document = vector('valid', 'q2-valve220-oblique.apmap');
  const wedge = brushesOf(document).find((brush) => brush.brush_id === 'brs_q2v04valve0');
  const slope = wedge.faces.find((candidate) => candidate.face_id === 'fac_q2v04valve0f5');
  assert.deepEqual(slope.projection.scale, [0.5, 2]);
  assert.equal(slope.projection.rotation, 15);
  assert.equal(valveToClassic(slope.plane.points, slope.projection), null);
  // Its five axial siblings convert; only the slope is refused.
  for (const candidate of wedge.faces.filter((f) => f !== slope))
    assert.ok(valveToClassic(candidate.plane.points, candidate.projection), candidate.face_id);
  // And the best classic projection a converter could guess is visibly wrong at the probes.
  const guess = { mode: 'classic', shift: [slope.projection.u_axis[3], slope.projection.v_axis[3]], rotation: 15, scale: [0.5, 2] };
  const worst = Math.max(...probes(slope.plane.points, 256).map((p) =>
    maxDelta(classicUV(slope.plane.points, guess, p), valveUV(slope.projection, p))));
  assert.ok(worst > 1, `a naive classic guess is off by ${worst} texels`);
});

// ---------------------------------------------------------------------------------------------
// Entities and editor metadata
// ---------------------------------------------------------------------------------------------

test('repeated and empty Quake II keys are content items in authored order, never an object', () => {
  const document = vector('valid', 'q2-entity-keys.apmap');
  const keys = (entity) => entity.content.filter((item) => item.kind === 'property').map((item) => [item.key, item.value]);
  const speaker = document.entities.find((entity) => entity.entity_id === 'ent_q2v06speak0');
  assert.deepEqual(keys(speaker).filter(([key]) => key === 'target'), [['target', 't1'], ['target', 't2']]);
  assert.deepEqual(keys(speaker).filter(([key]) => key === '_aulibs_unknown').length, 2);
  assert.deepEqual(keys(speaker).find(([key]) => key === 'message'), ['message', '']);
  assert.deepEqual(keys(document.entities[0]).filter(([key]) => key === 'sky').map(([, value]) => value), ['unit1_', 'unit2_']);
  // What casual flattening would do — and what it loses.
  const flattened = Object.fromEntries(keys(speaker));
  assert.equal(flattened.target, 't2');
  assert.ok(keys(speaker).length > Object.keys(flattened).length);
});

test('groups nest over Quake II entities and authorship is document data', () => {
  const document = vector('valid', 'q2-groups-entities.apmap');
  assert.equal(document.authorship.license, 'Apache-2.0');
  const outer = document.groups.find((group) => group.name === 'Lift room');
  assert.deepEqual(outer.members.map((member) => member.kind), ['group', 'entity']);
  const door = document.entities.find((entity) => entity.entity_id === 'ent_q2v05door00');
  assert.ok(door.content.some((item) => item.kind === 'brush'));
  // No entity was invented to hold the editor's grouping.
  assert.ok(document.entities.every((entity) =>
    !/^(func_group|group|layer|folder)\b/i.test(entity.content.find((item) => item.key === 'classname').value)));
});
