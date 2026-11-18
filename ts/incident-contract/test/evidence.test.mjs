// 1.5.0: the socket-loss evidence fields, at their authority.
//
// Five bounded integers an AUC socket-loss incident may carry. Every one gets a passing vector at
// each inclusive bound and a rejected vector just outside it, plus a string, a decimal, a numeric
// string and null — checked by BOTH the schema engine (ajv) and the dependency-free validator, which
// must agree. Redaction is then shown to keep a member only when it is a valid integer and to drop
// every member the schema does not declare, so no arbitrary supplied field is ever serialized.

import test from "node:test";
import assert from "node:assert/strict";
import Ajv2020 from "ajv/dist/2020.js";

import {
  envelopeSchema,
  createIncident,
  validateIncident,
  redactIncident,
  redactEvidence,
  toSentryEvent,
  toDiagnosticText,
} from "../src/index.mjs";

const ajv = new Ajv2020({ allErrors: true, strict: false });
const ajvValidate = ajv.compile(envelopeSchema);

const FIELDS = {
  close_code: [1000, 4999],
  connected_ms: [0, 86_400_000],
  bytes_ahead: [0, 2_147_483_647],
  frame_bytes: [0, 2_147_483_647],
  unanswered_ms: [0, 3_600_000],
};

function lossIncident(evidence) {
  return createIncident({
    severity: "warning",
    component: "AUC",
    subsystem: "room.client",
    code: "auc.disconnected",
    operation: "session.presence",
    message: "A collaborator's connection was lost without a normal close.",
    correlation_id: "0123456789abcdef0123456789abcdef",
    release: "1.161",
    environment: "test",
    recoverable: true,
    evidence,
  });
}

function verdicts(incident) {
  return { mine: validateIncident(incident).valid, theirs: ajvValidate(incident) };
}

test("the schema declares exactly the five 1.5.0 fields with the documented inclusive ranges", () => {
  const props = envelopeSchema.$defs.evidence.properties;
  for (const [field, [min, max]] of Object.entries(FIELDS)) {
    assert.equal(props[field].type, "integer", field);
    assert.equal(props[field].minimum, min, `${field} minimum`);
    assert.equal(props[field].maximum, max, `${field} maximum`);
  }
  assert.equal(envelopeSchema.$defs.evidence.additionalProperties, false);
});

for (const [field, [min, max]] of Object.entries(FIELDS)) {
  test(`${field}: both inclusive bounds pass, both validators agree`, () => {
    for (const value of [min, max, Math.floor((min + max) / 2)]) {
      const { mine, theirs } = verdicts(lossIncident({ [field]: value }));
      assert.equal(mine, true, `${field}=${value} refused by validateIncident`);
      assert.equal(theirs, true, `${field}=${value} refused by ajv`);
    }
  });

  test(`${field}: out of range, decimal, string, numeric string and null are refused by both`, () => {
    const bad = [min - 1, max + 1, min + 0.5, "1006", "fast", null, true, [min], { v: min }];
    for (const value of bad) {
      const { mine, theirs } = verdicts(lossIncident({ [field]: value }));
      assert.equal(mine, false, `${field}=${JSON.stringify(value)} accepted by validateIncident`);
      assert.equal(theirs, false, `${field}=${JSON.stringify(value)} accepted by ajv`);
    }
  });
}

test("all five together, with an older counter, is one valid envelope", () => {
  const incident = lossIncident({
    client_count: 1,
    close_code: 1006,
    connected_ms: 25_412,
    bytes_ahead: 393_216,
    frame_bytes: 1_048_576,
    unanswered_ms: 60_004,
  });
  const { mine, theirs } = verdicts(incident);
  assert.ok(mine && theirs, JSON.stringify(validateIncident(incident).errors));
});

test("omitting every new field is still valid, and so is an envelope with no evidence at all", () => {
  for (const incident of [lossIncident({ client_count: 2 }), lossIncident(undefined), lossIncident({})]) {
    const { mine, theirs } = verdicts(incident);
    assert.ok(mine && theirs);
  }
});

test("an unknown key next to the new fields is still refused", () => {
  const { mine, theirs } = verdicts(lossIncident({ close_code: 1006, close_reason: "peer vanished" }));
  assert.equal(mine, false);
  assert.equal(theirs, false);
});

test("an envelope written before 1.5.0 (no new fields) validates unchanged", () => {
  const before = createIncident({
    severity: "warning",
    component: "AUC",
    subsystem: "room.dispose",
    code: "auc.disconnected",
    message: "The room closed.",
    recoverable: true,
    evidence: { client_count: 3, queue_depth: 0, event_loop_lag_ms: 12 },
  });
  assert.ok(verdicts(before).mine && verdicts(before).theirs);
  assert.deepEqual(redactIncident(before).evidence, before.evidence);
});

test("redaction keeps a valid integer and OMITS a wrong-kind or out-of-range one, never zeroing it", () => {
  const out = redactEvidence({
    close_code: 1006,
    connected_ms: 1.5,
    bytes_ahead: "393216",
    frame_bytes: -1,
    unanswered_ms: 3_600_001,
    client_count: Number.NaN,
    queue_depth: 4,
  });
  assert.deepEqual(out, { close_code: 1006, queue_depth: 4 });
});

test("redaction never serializes an evidence member the schema does not declare", () => {
  const CANARY = "canary-7f3e-guest-name-alice@example.invalid";
  const incident = { ...lossIncident({ close_code: 1006 }) };
  incident.evidence = {
    close_code: 1006,
    user_name: CANARY,
    room_id: CANARY,
    ticket: CANARY,
    nested: { deep: CANARY },
    unanswered_ms: 61_000,
  };
  const safe = redactIncident(incident);
  assert.deepEqual(safe.evidence, { close_code: 1006, unanswered_ms: 61_000 });
  const event = toSentryEvent(incident);
  assert.ok(!JSON.stringify(event).includes("canary-7f3e"), "an undeclared member reached the event");
  assert.deepEqual(event.contexts.incident.evidence, { close_code: 1006, unanswered_ms: 61_000 });
  assert.ok(!toDiagnosticText(incident).includes("canary-7f3e"));
  assert.ok(validateIncident(safe).valid);
});

test("redaction drops a non-object evidence value rather than carrying it", () => {
  for (const value of ["text", 7, ["a"], null]) {
    const incident = { ...lossIncident(undefined), evidence: value };
    const safe = redactIncident(incident);
    assert.equal("evidence" in safe, false, `evidence=${JSON.stringify(value)} survived`);
  }
});

test("a string member survives redaction as a redacted string, a non-string one does not", () => {
  assert.deepEqual(redactEvidence({ http_reason: "session_full" }), { http_reason: "session_full" });
  assert.deepEqual(redactEvidence({ http_reason: 404 }), {});
});

test("the diagnostic text a user previews shows the new counters", () => {
  const text = toDiagnosticText(lossIncident({ close_code: 1006, unanswered_ms: 60_004 }));
  assert.match(text, /close_code\s+1006/);
  assert.match(text, /unanswered_ms\s+60004/);
});
