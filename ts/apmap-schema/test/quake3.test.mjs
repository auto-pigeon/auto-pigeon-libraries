/**
 * What 1.5 added: the Quake III surfaces — the `patch` content item, the `brush_primitives` face
 * projection and the `patch` group member — and the evidence behind their shape.
 *
 * Tests only. The texture-projection functions at the bottom are REFERENCE MATH for one decision
 * (keep a brushDef matrix verbatim rather than convert it to `classic`), written from Q3Map2's own
 * definitions so the decision is reproducible; nothing here is a runtime surface.
 */
import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import {
  compile, compileDef, describeErrors, loadCurrentSchema, semanticFaults, vector, vectorIndex,
} from './helpers.mjs';

const current = loadCurrentSchema();
const validateCurrent = compile(current);
const index = vectorIndex();
const Q3_VALID = index.valid.map((entry) => entry.file).filter((file) => file.startsWith('q3-'));
const vectorBytes = (kind, file) => fs.readFileSync(path.join(import.meta.dirname, '..', 'test-vectors', kind, file), 'utf8');
const patchesOf = (document) => document.entities.flatMap((entity) => entity.content.filter((item) => item.kind === 'patch'));

// ---------------------------------------------------------------------------------------------
// The patch item
// ---------------------------------------------------------------------------------------------

test('a patch is a closed content item in canonical member order', () => {
  const def = current.$defs.patch_item;
  assert.equal(def.additionalProperties, false);
  assert.deepEqual(def.required, ['kind', 'patch_id', 'texture', 'width', 'height', 'control_points']);
  assert.deepEqual(Object.keys(def.properties),
    ['kind', 'patch_id', 'derived_from', 'texture', 'width', 'height', 'control_points', 'tail', 'extensions']);
  assert.deepEqual(current.$defs.content_item.oneOf.map((arm) => arm.$ref),
    ['#/$defs/property_item', '#/$defs/brush_item', '#/$defs/patch_item']);
  // A shader name follows exactly the face `texture` rule, so one texture tool serves both.
  assert.deepEqual({ ...def.properties.texture, description: undefined }, { ...current.$defs.face.properties.texture, description: undefined });
});

test('a patch dimension is odd, from 3 to 31, and nothing else', () => {
  const validate = compileDef(current, 'patch_dimension');
  const legal = Array.from({ length: 15 }, (_, i) => 3 + 2 * i);
  assert.deepEqual(current.$defs.patch_dimension.enum, legal);
  for (let n = -1; n <= 40; n += 1) assert.equal(validate(n), legal.includes(n), String(n));
  for (const bad of [3.5, '3', null, 31.000001]) assert.equal(validate(bad), false, JSON.stringify(bad));
});

test('a control point is five bounded numbers, and the world bound catches non-finite values', () => {
  const validate = compileDef(current, 'patch_point');
  assert.equal(validate([0, 0, 0, 0, 0]), true);
  assert.equal(validate([131072, -131072, 0, 131072, -131072]), true, 'the bound is inclusive');
  for (const bad of [[0, 0, 0, 0], [0, 0, 0, 0, 0, 0], [131072.5, 0, 0, 0, 0], [0, 0, 0, 0, Infinity],
                     [0, 0, 0, 0, -Infinity], [0, 0, 0, 0, Number.NaN], [0, 0, '0', 0, 0]])
    assert.equal(validate(bad), false, JSON.stringify(bad));
  // `1e999` in JSON text decodes to Infinity in JavaScript, and the vector proves the schema refuses it.
  const nonfinite = JSON.parse(vectorBytes('invalid', 'patch-point-nonfinite.apmap'));
  assert.equal(patchesOf(nonfinite)[0].control_points[0][0][3], Infinity);
});

test('the largest legal patch, 31x31, is valid, and one column more is not', () => {
  const base = vector('valid', 'q3-patch-minimal.apmap');
  const grid = Array.from({ length: 31 }, (_, c) => Array.from({ length: 31 }, (_, r) => [c * 8, r * 8, (c * r) % 17, c / 30, r / 30]));
  const document = structuredClone(base);
  Object.assign(patchesOf(document)[0], { width: 31, height: 31, control_points: grid });
  assert.ok(validateCurrent(document), describeErrors(validateCurrent.errors));
  assert.deepEqual(semanticFaults(document), []);
  const over = structuredClone(document);
  patchesOf(over)[0].control_points.push(grid[0]);
  assert.equal(validateCurrent(over), false, '32 columns is over the hard limit whatever `width` says');
});

test('control_points is in patchDef2 text order: control_points[c][r] is verts[r * width + c]', () => {
  // Q3Map2's ParsePatch reads `width` parenthesised groups of `height` points and stores the
  // j-th group's i-th point at verts[i * width + j]. Write the grid out in that order, read it
  // back with that loop, and the two indexings must agree for every point.
  for (const file of Q3_VALID) {
    for (const patch of patchesOf(vector('valid', file))) {
      const { width, height } = patch;
      const text = `( ${patch.control_points.map((column) => `( ${column.map((p) => `( ${p.join(' ')} )`).join(' ')} )`).join(' ')} )`;
      const numbers = text.replace(/[()]/g, ' ').trim().split(/\s+/).map(Number);
      const verts = new Array(width * height);
      let at = 0;
      for (let j = 0; j < width; j += 1)
        for (let i = 0; i < height; i += 1) { verts[i * width + j] = numbers.slice(at, at + 5); at += 5; }
      for (let c = 0; c < width; c += 1)
        for (let r = 0; r < height; r += 1)
          assert.deepEqual(verts[r * width + c], patch.control_points[c][r], `${file} ${patch.patch_id} [${c}][${r}]`);
    }
  }
});

test('a derived patch id is the section 3.5 digest of kind "patch" and its source path', () => {
  const document = vector('valid', 'q3-patch-derived-ids.apmap');
  assert.equal(document.id_policy, 'derived');
  const digest = (prefix, kind, ...components) =>
    `${prefix}_${crypto.createHash('sha256').update(['apmap/1.0', kind, ...components].join('\u001f')).digest('hex').slice(0, 16)}`;
  const entity = document.entities[0];
  const check = (id, prefix, kind, from) => assert.equal(id, digest(prefix, kind, from.source_map, from.source_sha256, from.source_path), id);
  check(entity.entity_id, 'ent', 'entity', entity.derived_from);
  for (const item of entity.content) {
    if (item.kind === 'brush') {
      check(item.brush_id, 'brs', 'brush', item.derived_from);
      for (const face of item.faces) check(face.face_id, 'fac', 'face', face.derived_from);
    }
    if (item.kind === 'patch') {
      assert.equal(item.derived_from.source_path, 'e0.p0');
      check(item.patch_id, 'pat', 'patch', item.derived_from);
    }
  }
});

test('a patch takes part in groups like any object an entity owns', () => {
  const member = compileDef(current, 'group_member');
  assert.equal(member({ kind: 'patch', patch_id: 'pat_q3arch00000001' }), true);
  assert.equal(member({ kind: 'patch', brush_id: 'brs_q3arch00000001' }), false, 'a patch member names a patch_id');
  assert.equal(member({ kind: 'patch', patch_id: 'pat_q3arch00000001', label: 'x' }), false, 'a member is a reference only');
  const nested = vector('valid', 'q3-patch-in-nested-group.apmap');
  assert.deepEqual(semanticFaults(nested), []);
  assert.ok(nested.groups.some((group) => group.members.some((m) => m.kind === 'group'))
    && nested.groups.some((group) => group.members.some((m) => m.kind === 'patch')));
});

test('patches keep their place among an entity\'s content items', () => {
  const door = vector('valid', 'q3-brush-primitives.apmap').entities[1];
  assert.deepEqual(door.content.map((item) => item.kind === 'property' ? `${item.kind}:${item.key}` : item.kind),
    ['property:classname', 'brush', 'patch', 'property:targetname']);
});

// ---------------------------------------------------------------------------------------------
// The brush_primitives projection
// ---------------------------------------------------------------------------------------------

test('a brush_primitives projection is a closed 2x3 matrix and nothing else', () => {
  const def = current.$defs.projection_brush_primitives;
  assert.equal(def.additionalProperties, false);
  assert.deepEqual(def.required, ['mode', 'matrix']);
  assert.deepEqual(Object.keys(def.properties), ['mode', 'matrix']);
  const validate = compileDef(current, 'projection');
  const good = { mode: 'brush_primitives', matrix: [[0.0078125, 0, 0], [0, 0.0078125, 0]] };
  assert.equal(validate(good), true);
  assert.equal(validate({ ...good, rotation: 0 }), false, 'no classic member rides along');
  assert.equal(validate({ mode: 'brush_primitives', matrix: [[1, 0, 0]] }), false, 'one row');
  assert.equal(validate({ mode: 'brush_primitives', matrix: [[1, 0, 0], [0, 1, Infinity]] }), false, 'non-finite');
  assert.equal(validate({ mode: 'classic', matrix: good.matrix, shift: [0, 0], rotation: 0, scale: [1, 1] }), false);
});

test('SEM-Q-3 is exact arithmetic on the numbers as written', () => {
  const base = vector('valid', 'q3-brush-primitives.apmap');
  const withMatrix = (matrix) => {
    const document = structuredClone(base);
    for (const face of document.entities[0].content[1].faces) face.projection.matrix = matrix;
    return semanticFaults(document).map((fault) => fault.rule);
  };
  assert.deepEqual(withMatrix([[0.0078125, 0, 0], [0, -0.0078125, 0]]), [], 'a mirror is fine');
  assert.deepEqual(withMatrix([[0.0078125, 0.0078125, 0], [0, 0.0078125, 0]]), [], 'a shear is fine');
  assert.ok(withMatrix([[0, 0, 0.5], [0, 0, 0.5]]).includes('SEM-Q-3'), 'all zero');
  assert.ok(withMatrix([[1, 2, 0], [2, 4, 0]]).includes('SEM-Q-3'), 'rank one');
});

// ---------------------------------------------------------------------------------------------
// SER-7Q — the precision a Quake III surface needs
// ---------------------------------------------------------------------------------------------

test('SER-7Q: every Quake III vector is a canonical fixed point with its fine coordinates intact', () => {
  assert.ok(Q3_VALID.length >= 5);
  for (const file of Q3_VALID) {
    const bytes = vectorBytes('valid', file);
    assert.equal(`${JSON.stringify(JSON.parse(bytes), null, 2)}\n`, bytes, file);
    assert.ok(!/^\s*-?\d+(\.\d+)?[eE]/m.test(bytes), `${file} uses exponent notation`);
  }
  const minimal = patchesOf(vector('valid', 'q3-patch-minimal.apmap'))[0];
  assert.deepEqual(minimal.control_points[0][0].slice(3), [0.0078125, 0.00260417]);
});

test('SER-7 six-place rounding would change a patch texture coordinate and a texture matrix', () => {
  // Why the Quake III fields carry their own rule: SER-7 rounds to six decimal places, and the
  // values Radiant writes (six significant digits, not six places) do not survive it.
  const ser7 = (value) => Math.round(value * 1e6) / 1e6;
  for (const value of [0.0078125, 0.00260417, 0.00676582, 1.0078125])
    assert.notEqual(ser7(value), value, `${value} survives SER-7, so it proves nothing`);
  // The damage is visible: at x' = 4096 a 1/128 matrix coefficient rounded by SER-7 moves the
  // texture by more than a quarter of a texel on a 128-pixel image.
  assert.ok(Math.abs((ser7(0.0078125) - 0.0078125) * 4096 * 128) > 0.25);
});

// ---------------------------------------------------------------------------------------------
// Can a brushDef matrix be written as `classic` instead? The numerical answer.
//
// If it could, without loss, the simpler shape would win and 1.5 would need no new projection.
// Reference math from Q3Map2 (map.c `QuakeTextureVecs`, `TextureAxisFromPlane`; brush primitives
// `ComputeAxisBase`), with texture coordinates in texture units, as Q3Map2 computes them:
//
//   classic:          s = (dot(p, vecs[0]) + shift[0]) / imageWidth,  t likewise with height
//   brush primitives: s = m00 * dot(p, texS) + m01 * dot(p, texT) + m02, t likewise
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

function textureAxisFromPlane(normal) {
  let best = 0;
  let bestDot = 0;
  BASE_AXIS.forEach(([axis], i) => { const d = dot(normal, axis); if (d > bestDot) { bestDot = d; best = i; } });
  return [BASE_AXIS[best][1], BASE_AXIS[best][2]];
}

function quakeTextureVecs(normal, { shift, rotation, scale }) {
  const vecs = textureAxisFromPlane(normal).map((v) => [...v]);
  const angle = rotation / 180 * Math.PI;
  const [sin, cos] = [Math.sin(angle), Math.cos(angle)];
  const sv = vecs[0].findIndex((x) => x !== 0);
  const tv = vecs[1].findIndex((x) => x !== 0);
  for (const v of vecs) [v[sv], v[tv]] = [cos * v[sv] - sin * v[tv], sin * v[sv] + cos * v[tv]];
  return { vecs: vecs.map((v, i) => v.map((x) => x / scale[i])), shift, sv, tv };
}

function computeAxisBase(normal) {
  const n = normal.map((x) => (Math.abs(x) < 1e-6 ? 0 : x));
  const rotY = -Math.atan2(n[2], Math.sqrt(n[1] * n[1] + n[0] * n[0]));
  const rotZ = Math.atan2(n[1], n[0]);
  return [[-Math.sin(rotZ), Math.cos(rotZ), 0],
          [-Math.sin(rotY) * Math.cos(rotZ), -Math.sin(rotY) * Math.sin(rotZ), -Math.cos(rotY)]];
}

/** Three non-collinear points of the face plane, spanning it along the brush-primitives axes. */
function planeSamples(points) {
  const [texS, texT] = computeAxisBase(normalOf(points));
  const origin = points[1];
  return [origin, origin.map((x, i) => x + 64 * texS[i]), origin.map((x, i) => x + 64 * texT[i])];
}

const classicST = (normal, projection, [w, h], p) => {
  const { vecs, shift } = quakeTextureVecs(normal, projection);
  return [(dot(p, vecs[0]) + shift[0]) / w, (dot(p, vecs[1]) + shift[1]) / h];
};
const matrixST = (normal, m, p) => {
  const [texS, texT] = computeAxisBase(normal);
  const [x, y] = [dot(p, texS), dot(p, texT)];
  return [m[0][0] * x + m[0][1] * y + m[0][2], m[1][0] * x + m[1][1] * y + m[1][2]];
};

/** Solve a 3x3 system by Cramer's rule. */
function solve3(a, b) {
  const det = (m) => m[0][0] * (m[1][1] * m[2][2] - m[1][2] * m[2][1]) - m[0][1] * (m[1][0] * m[2][2] - m[1][2] * m[2][0])
    + m[0][2] * (m[1][0] * m[2][1] - m[1][1] * m[2][0]);
  const d = det(a);
  return [0, 1, 2].map((col) => det(a.map((row, r) => row.map((x, c) => (c === col ? b[r] : x)))) / d);
}

/** classic -> matrix: the matrix that gives the same texture coordinates everywhere on the plane. */
function classicToMatrix(points, projection, size) {
  const normal = normalOf(points);
  const [texS, texT] = computeAxisBase(normal);
  const samples = planeSamples(points);
  const rows = samples.map((p) => [dot(p, texS), dot(p, texT), 1]);
  const st = samples.map((p) => classicST(normal, projection, size, p));
  return [solve3(rows, st.map((v) => v[0])), solve3(rows, st.map((v) => v[1]))];
}

/** matrix -> classic: the best classic projection, and how far its re-derived matrix lands off. */
function matrixToClassic(points, matrix, [w, h]) {
  const normal = normalOf(points);
  const { sv, tv } = quakeTextureVecs(normal, { shift: [0, 0], rotation: 0, scale: [1, 1] });
  const [base0, base1] = textureAxisFromPlane(normal);
  const samples = planeSamples(points);
  // In pixels, classic s is a0 . p[sv,tv] + shift: three unknowns, three samples on the plane.
  const rows = samples.map((p) => [p[sv], p[tv], 1]);
  const px = samples.map((p) => matrixST(normal, matrix, p));
  const [a0sv, a0tv, shift0] = solve3(rows, px.map((v) => v[0] * w));
  const [a1sv, a1tv, shift1] = solve3(rows, px.map((v) => v[1] * h));
  const theta = Math.atan2(a0tv * base0[sv], a0sv * base0[sv]);
  const scale0 = base0[sv] / Math.hypot(a0sv, a0tv) * Math.sign(base0[sv]);
  const scale1 = base1[tv] / (-Math.sin(theta) * a1sv + Math.cos(theta) * a1tv);
  const projection = { shift: [shift0, shift1], rotation: theta * 180 / Math.PI, scale: [scale0, scale1] };
  const back = classicToMatrix(points, projection, [w, h]);
  const error = Math.max(...back.flatMap((row, r) => row.map((x, c) => Math.abs(x - matrix[r][c]))));
  return { projection, error };
}

const FACES = {
  floor: [[0, 64, 0], [64, 64, 0], [64, 0, 0]],
  wall: [[64, 0, 64], [64, 64, 64], [64, 64, 0]],
  slope: [[0, 0, 0], [64, 0, 32], [64, 64, 48]],
  steep: [[0, 0, 0], [16, 64, 128], [64, 16, 96]],
};
const CLASSICS = [
  { shift: [0, 0], rotation: 0, scale: [0.5, 0.5] },
  { shift: [17, -9], rotation: 30, scale: [0.5, 1.25] },
  { shift: [-3.5, 40], rotation: 137, scale: [-0.25, 0.5] },
  { shift: [8, 8], rotation: -45, scale: [1, -2] },
  { shift: [0, 64], rotation: 90, scale: [-1, -1] },
];

test('classic -> matrix -> classic round-trips on rotated and mirrored faces, given the image size', () => {
  let worst = 0;
  for (const [name, points] of Object.entries(FACES))
    for (const projection of CLASSICS) {
      const matrix = classicToMatrix(points, projection, [128, 64]);
      const { projection: recovered, error } = matrixToClassic(points, matrix, [128, 64]);
      worst = Math.max(worst, error);
      assert.ok(error < 1e-9, `${name} ${JSON.stringify(projection)}: ${error}`);
      // The recovered parameters may be a different spelling of the same mapping (rotation + 180
      // with both scales negated); what must agree is the texture coordinate at every point.
      const normal = normalOf(points);
      for (const p of [...planeSamples(points), [7, -3, 11]])
        classicST(normal, projection, [128, 64], p).forEach((value, i) =>
          assert.ok(Math.abs(value - classicST(normal, recovered, [128, 64], p)[i]) < 1e-9, `${name} at ${p}`));
    }
  assert.ok(worst < 1e-9);
});

test('but the matrix depends on the image size, which an APMap document does not hold', () => {
  for (const points of Object.values(FACES)) {
    const a = classicToMatrix(points, CLASSICS[1], [128, 64]);
    const b = classicToMatrix(points, CLASSICS[1], [256, 256]);
    assert.ok(Math.abs(a[0][0] - b[0][0]) + Math.abs(a[1][1] - b[1][1]) > 1e-4,
      'the same classic face maps to a different matrix on a different image');
  }
});

test('and a sheared matrix has no classic form at all — so 1.5 keeps the matrix verbatim', () => {
  const sheared = vector('valid', 'q3-brush-primitives.apmap').entities[0].content[1].faces[3].projection.matrix;
  assert.deepEqual(sheared, [[0.0078125, 0.00390625, 0], [0, 0.0078125, 0.125]]);
  for (const [name, points] of Object.entries(FACES)) {
    const { error } = matrixToClassic(points, sheared, [128, 128]);
    assert.ok(error > 1e-3, `${name}: a sheared matrix round-tripped through classic (${error})`);
  }
});
