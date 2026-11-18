# `@auto-pigeon/incident-contract`

The cross-stack incident contract: **one envelope**, **one taxonomy of codes**, **one
correlation-id convention**, and **one set of redaction rules**, shared by every Auto-Pigeon
component that can fail in a way a person notices.

It ships contract data plus a dependency-free reference implementation of the rules that data
needs. It has **no transport**: no SDK, no DSN, no HTTP client, nothing that starts. Which error
backend a component reports to, and how, is that component's decision — this package decides what a
report *is*.

## Why the package exists

Six components can each be the reason an editor session goes wrong, and a user only ever sees one
of them. Without a shared shape, "the paste froze" is a browser console line in one repository, a
Go log line in another and a Colyseus disconnect reason in a third — three vocabularies for one
event, and no way to tell they are the same event. The envelope and the correlation id are what
turn those into one story; the code taxonomy is what makes *"which errors does this product have"*
answerable without reading six repositories.

## The envelope is CLOSED, and that is the design

`schema/incident-envelope-1.0.schema.json` sets `additionalProperties: false` at every level, and
`evidence` is a fixed set of typed counters — face counts, an HTTP status, a queue depth, a worker
attempt. There is **no field a map document, a request body, an annotation or a prefab can legally
occupy**. That is deliberate: an incident may end up in a public bug report, and a bound that a
reviewer has to enforce by reading every call site is not a bound. Something the schema cannot
express is a schema change, reviewed here.

```js
import { createIncident, validateIncident, redactIncident, toSentryEvent } from "@auto-pigeon/incident-contract";

const incident = createIncident({
  severity: "warning",
  component: "AUP",
  subsystem: "render.3d",
  code: "render.3d.slow",
  operation: "Paste",
  message: "3D redraw is still running after Paste.",
  duration_ms: 3100,
  correlation_id: correlationId,      // the one this user action has carried all along
  release: "1.842",                   // PREPROD-05's 1.<commit-count>, or "unknown"
  environment: "production",
  recoverable: true,
  user_action: "You can wait longer or reload the page.",
  evidence: { face_count: 31040 },
});

validateIncident(incident);           // { valid, errors } — returns, never throws
const event = toSentryEvent(incident);  // redacted on the way out
```

`createIncident` fills in only what a caller cannot reasonably repeat at every call site — the
schema version, an id, a timestamp. It invents **no** component, **no** severity and **no** release:
a made-up release is the placeholder-version failure PREPROD-05 settled, and `"unknown"` is a legal,
meaningful value here.

## Two identities: the incident, and the transmission

An incident has **two** ids and they answer different questions.

| | | |
| --- | --- | --- |
| `incident_id` | one FAULT, in the product's terms | **stable** for the whole lifecycle of that fault — every rung of an escalation, and its recovery |
| `event_id` | one TRANSMISSION to the error backend | **fresh on every `toSentryEvent` call**, minted by `newTransportEventId()` |

They were the same value until INC-01, and that quietly cost the product most of what it reported.
GlitchTip's store endpoint answers **HTTP 422 `Duplicate event id`** to the second event carrying an
id it already holds, so a stall that escalated 3 s → 10 s → 30 s → recovered delivered its *first*
rung and had the other three refused at the door; OBS-07 measured 22 rejected sends in a single
stage. The stable id was doing its job perfectly — one row in the buffer, one card on screen, one
thing to talk about — and that is precisely why it could not also be the transport id.

Nothing about the incident changed. The stable id still travels on every event, twice:

```js
const event = toSentryEvent(incident);
event.event_id                        // fresh, different on the next call
event.tags.incident_id                // the stable one — SEARCHABLE in GlitchTip
event.contexts.incident.incident_id   // the stable one — in the closed, bounded block
```

The tag is the half a person queries (`incident_id:<32 hex>` finds every rung of one stall); the
context is the half the closed schema bounds. Both are safe to publish for the same reason the
correlation id is: 128 random bits encoding nothing.

`newTransportEventId()` mints a **version-4 UUID as 32 lowercase hex characters, no dashes** —
Sentry's documented shape for the field. Today's GlitchTip would accept any 32 hex characters, which
is why the old code worked at all; satisfying the documented contract rather than one server
version's tolerance is what survives an upgrade. It uses `crypto.getRandomValues` and never
`crypto.randomUUID`, which exists only in a secure context — AUP is served over plain HTTP on a LAN.

### One fault is one issue

`toSentryEvent` also sets `fingerprint: [component, code, subsystem, operation]`. Once repeated
transmissions stop being rejected, the rungs of one stall are events whose *messages* differ
("after 3 seconds", "after 10 seconds"), and default grouping would file them as unrelated issues —
trading a rejected send for a scattered one. Grouping on what the fault *is* is what AUB's and AUE's
Go reporters have always done; this is the line that makes the three lanes agree. Deliberately not
the message: a message carrying a duration or a map name groups every occurrence separately.

## The correlation id

**128 random bits, 32 lowercase hex characters**, minted where a *person* does something and then
carried unchanged for as far as that action reaches:

```text
AUP  mints it at the interaction boundary
 |    HTTP:  X-Auto-Pigeon-Correlation-Id: <id>
 v
AUB  reads the header, puts it on the job it schedules and on its own incidents
 |    job record: correlation_id
 v
AUE  reads it from the job it was handed

AUP  ---- Colyseus message field `correlationId` ---->  AUC
```

Every incident along that path carries it, and so does the GlitchTip event, as the tag
`correlation_id`. It encodes **nothing** — no user, no account, no map, no host, no timestamp —
because it is an unauthenticated value that will appear in public bug reports.

`correlationIdFromHeaders` reads it from a `Headers`, a plain object or node's `IncomingMessage`
headers, and treats a malformed value as absent rather than forwarding it.

## Redaction

`schema/redaction-rules.json` is the authority and `src/redact.mjs` is the reference reader, so a Go
implementation in AUB/AUE and a Python one in the tooling are *the same rules* rather than three
sincere transcriptions. Every pattern avoids lookaround and backreferences, so it compiles unchanged
in JavaScript, Go's RE2 and Python's `re`.

Three kinds of rule, applied in order:

| | rule | example |
| --- | --- | --- |
| 1 | **dropped keys** — exact key name, whole value gone, at any depth | `geometry`, `annotations`, `prefab`, `body`, `headers` |
| 2 | **denied key substrings** — value replaced | `auth`, `cookie`, `token`, `password`, `email`, `invite` |
| 3 | **value patterns** — over every surviving string | e-mail, JWT, `Bearer …`, `pb_auth=…`, URL user-info, absolute paths |

Two consequences worth stating outright, because both are load-bearing:

- `face_count` survives and `faces` does not. Dropped keys match **exactly** for that reason: the
  counter is evidence, the array is a map.
- **Nothing matches a bare 32-character hex string.** `incident_id` and `correlation_id` are exactly
  that shape, and a redacted record nobody can correlate is a record nobody can act on.

Order is part of the contract: `url-userinfo` runs before `email`, because
`https://key:secret@host/3` contains something the e-mail rule matches, and a credential removed by
the wrong rule is indistinguishable from correct until the day a URL has no password in it.

`test/redaction.test.mjs` proves all of it with **fake secrets** — invented for that file, valid
nowhere.

## Validation is on demand, and it returns

AULIBS rule 2.1. Nothing here runs on import, on construction or on a timer, and `validateIncident`
answers `{ valid, errors }` rather than throwing: an incident is usually reported from a path that
is *already* failing, and a validator that threw would turn a report about a bug into a second bug.

`src/validate.mjs` is a reader for the subset of JSON Schema the envelope uses, driven by the schema
file itself — adding a field needs no edit there. It is allowed to exist beside a real schema engine
only because `test/contract.test.mjs` runs **ajv** over the same 25 cases and fails if the two
disagree.

## The public bug target

`BUG_REPORT_URL` is `https://github.com/auto-pigeon/bug-reports/issues`, recorded once, here.
Nothing in this package opens it, submits to it or creates an issue: a report is something a person
chooses to send after reading exactly what it contains — which is what `toDiagnosticText` renders,
redacted, so the preview and the payload are the same string.

## Layout

```text
ts/incident-contract/
├── schema/
│   ├── incident-envelope-1.0.schema.json   the closed envelope — the authority
│   ├── incident-codes.json                 the canonical taxonomy
│   └── redaction-rules.json                the central redaction rules
├── src/
│   ├── index.mjs        the public surface
│   ├── index.d.ts       hand-written types (there is no build step)
│   ├── rules.mjs        loads the contract data; SCHEMA_VERSION, BUG_REPORT_URL
│   ├── codes.mjs        taxonomy lookups
│   ├── correlation.mjs  the correlation-id convention
│   ├── redact.mjs       central redaction
│   ├── validate.mjs     on-demand validation
│   └── envelope.mjs     createIncident, toSentryEvent, toDiagnosticText, transport ids
└── test/
    ├── contract.test.mjs   schema, taxonomy, correlation, ajv parity
    ├── evidence.test.mjs   1.5.0 evidence bounds (ajv parity) and evidence redaction
    └── redaction.test.mjs  fake secrets in, nothing out
```

Run it with `./run.sh test` from the repository root, or `npm test` here.

## The user bug report — `auto-pigeon-bug-report/1.1` (since 1.4.0; 1.0 since 1.2.0)

A bug report is **published publicly** on `https://github.com/auto-pigeon/bug-reports`, so it is
ONE document with ONE set of renderings, and nothing is added after the user has seen it.

| | |
| --- | --- |
| `schema/bug-report-1.1.schema.json` | the CLOSED document: user summary/steps/expected/actual, component, **report type and area**, release, environment, an incident by **code** (never its message), coarse client facts, and at most 30 recent machine-named activities |
| `schema/bug-report-1.0.schema.json` | the previous version, which a server may still accept until `previous_schemas[].accepted_until` |
| `schema/bug-report-rules.json` | bounds, the removed code points (C0/C1, bidi, zero-width), the patterns, and since 1.4.0 the classification: report types and their headings, areas, which areas each application offers, the incident-to-area mapping, the previous version's acceptance bound and the exact GitHub label catalogue — read by AUB's Go re-validation too |
| `fixtures/bug-report-vectors.json` | generated cross-language vectors, including redaction canaries in every prohibited source (`npm run vectors`) |

```js
import { buildBugReport, renderReportText, reportJsonDownload, renderIssue, prefilledIssueUrl } from "@auto-pigeon/incident-contract";

const built = buildBugReport({ component: "AUP", reportType: "bug", area: "editor", release, environment, user: { summary, steps, expected, actual }, incident, client, recent });
if (built.ok) {
  renderReportText(built.document);                     // the preview text and the .txt download
  reportJsonDownload(built.document);                   // the .json download (canonical, key-sorted)
  prefilledIssueUrl(built.document);                    // GitHub's prefilled form — opens, never submits
  renderIssue(built.document, { route: "server" });     // exactly what AUB's POST /api/bug-reports files
  bugReportLabels(built.document);                      // ["AUP", "Bug", "Area: Editor"] — the ONLY label derivation
}
```

- **Structural exclusion first.** There is no field for a map, map name, annotation, chat, asset,
  account, address or token. Redaction (now including full URLs, IPv4 addresses, known token
  prefixes and `~/` paths) is the second line, for what the user typed.
- **Nothing a user types can reshape the issue.** The whole report sits in one code fence longer
  than any backtick run inside it, so Markdown, HTML, mentions and issue references are inert.
- **A too-long report omits its OLDEST activity from the document itself** to fit GitHub's
  prefilled URL, so the preview still equals what is sent; when even that is not enough,
  `prefill.fits` is false and the client offers the download and the server route instead.
- **`validateBugReport` refuses a non-canonical document** (`user.steps:not_canonical`), which is
  how AUB declines to publish text the client did not already show the user.
- `report_id` is minted once per report and is the server route's idempotency key.

## 1.5.0 — socket-loss evidence, and redaction that keeps only declared evidence

The envelope id stays `auto-pigeon-incident/1.0` (`schema_version` `"1.0"`); `evidence` gains five
OPTIONAL bounded integers, so every envelope valid under 1.4.0 is still valid. They exist for AUC's
one warning about an admitted collaboration socket that was lost (`auc.disconnected`, subsystem
`room.client`; AUC #15, AULIBS #16).

| field | inclusive range | meaning |
| --- | --- | --- |
| `close_code` | 1000–4999 | the WebSocket close code the transport actually observed |
| `connected_ms` | 0–86400000 | measured connected duration |
| `bytes_ahead` | 0–2147483647 | outstanding observed send-buffer bytes at departure |
| `frame_bytes` | 0–2147483647 | one documented observed frame size (the snapshot frame during a join) |
| `unanswered_ms` | 0–3600000 | measured age of an unanswered liveness ping |

A field with no observation is **omitted**, never written as `0`: a zero is a claim that something
was measured.

`redactIncident` (and so `toSentryEvent` and `toDiagnosticText`) now passes `evidence` through the
new `redactEvidence`: only members the schema declares survive, an integer member only as a whole
number inside its schema range, a string member only as a redacted string. A wrong-kind, decimal,
out-of-range or undeclared member is omitted, so no arbitrary supplied field is serialized on the way
out. The ranges are read from the schema, not restated. `test/evidence.test.mjs` holds the passing
and refused boundary vectors for every field, checked by both `validateIncident` and ajv.

AUB and AUE embed the envelope schema byte for byte and must re-embed it; AUG and AUC vendor the
whole package and AUCOM's page vendors `schema/` and `src/`; each must re-vendor.

## 1.4.0 — every report is classified: application, type, area (`auto-pigeon-bug-report/1.1`)

Every report filed on `auto-pigeon/bug-reports` carries **exactly three labels**: its application
(`AUP`, `AUG` or `AUCOM` — the `component`, never user-editable), its type (`Bug` or `Feature
request`) and one primary area (`Area: Editor` … `Area: Other`). There are no workflow labels; the
only other label, `Already tracked elsewhere`, is applied by hand when closing.

- The document gains two required closed fields, `report_type` (`bug | feature_request`) and `area`
  (one machine id). `kind` still means only cold versus incident. The schema refuses an area the
  reporting application does not offer (`bugReportAreasFor(component)`), and the TypeScript types
  make it a compile error (`BugReportDocumentOf<"AUG">["area"]`).
- `bugReportLabels(document)` is the one derivation: AUB's server route, the prefilled URL and every
  test use it or the same data. It returns `null` — never a partial list — for anything it cannot
  classify exactly, and never takes a label string from a client.
- An incident-triggered report defaults to `bug` and to `suggestBugReportArea(component, incident)`:
  an exact subsystem entry for the application, else the exact incident-code entry (every code in the
  taxonomy is mapped; a test fails when one is not), else `other`. The user may change both.
- A feature request renders **Use case / Desired result / Current limitation** in place of
  **Steps to reproduce / Expected result / Actual result** (`bugReportHeadings(type)`); the bounded
  fields, their limits and their redaction are unchanged.
- `prefilledIssueUrl` adds GitHub's `labels` query parameter, and its length counts toward
  `prefill_url`. GitHub applies that parameter only for a reporter allowed to label issues in the
  repository, so for ordinary visitors the prefilled issue may arrive unlabelled; the server route is
  the path whose three labels are guaranteed.
- **Rolling compatibility.** `validateBugReport(doc, { acceptPrevious: true })` also accepts a
  `auto-pigeon-bug-report/1.0` document, and `bugReportSchemaStatus(schema, now)` says whether it is
  still inside `previous_schemas[0].accepted_until` (2026-11-30T23:59:59.999Z). `bugReportLabels`
  labels such a document `Bug` + `Area: Other`. Clients never emit 1.0.
- `label_catalogue` lists the exact labels with deterministic colours and descriptions, for the
  repository bootstrap.

AUB embeds the schemas, the rules and the vectors, and must re-embed them (including the new
`bug-report-1.1.schema.json`). AUG and AUC vendor the whole package, and AUCOM vendors `schema/` and
`src/`; each must re-vendor it.

## 1.3.0 — AUCOM files bug reports

`AUCOM` joins the bug report's `component` (in `bug-report-rules.json` and the schema enum), so the
Companion's page can build, preview and file the same `auto-pigeon-bug-report/1.0` document AUP and
AUG do; the vectors gain an `AUCOM cold report`. Only the permitted value changed — the document's
shape, bounds and renderings did not. AUB embeds both files and the vectors and must re-embed them;
AUG and AUC vendor the whole package and AUCOM vendors `schema/` and `src/`, and each must re-vendor.

## Stack frames for source-map symbolication (since 1.2.0)

`stackFrames(error.stack, { origin: location.origin })` keeps only frames from the application's
OWN bundle and rewrites each to `app:///assets/<file>.js` with a plain function name, line and
column — no host, IP, port, query, fragment, extension or foreign script. `toSentryEvent(incident,
{ exception: { type: exceptionType(error), frames } })` attaches them AFTER redaction, with the
incident CODE as the exception value (never the message). GlitchTip matches a frame to its privately
uploaded map by the file's basename under the event's exact release, so the `app://` prefix costs
nothing and publishes nothing.

## 1.2.0 taxonomy additions

`AUCOM` joins the envelope's `component` enum, with `aucom.job_failed` and
`aucom.readiness_failed`; the launcher gains `aut.launch_failed` (`subsystem`: compose, readiness,
child). AUB and AUE embed `incident-codes.json` and the envelope schema byte for byte and must
re-embed them; AUG and AUC vendor the whole package and must re-vendor it.
