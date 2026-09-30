# Auto-Pigeon Libraries

Shared libraries for the Auto-Pigeon project — TypeScript now, Go later —
consumed by the Auto-Pigeon editor, backend, collaboration server, gallery, and
extractor.

This repository ships libraries. It is not a service and has no runtime of its
own.

## Licence

Apache License 2.0. See [`LICENSE`](LICENSE).

## Validation happens only when you ask for it

Functions in these libraries may check a map — its schema, the convexity of its
brushes, holes in its geometry, anything else — **when you call them**. Nothing
here validates on its own: not when a document is loaded, not when it is saved,
not on a timer, not in a background worker. This is a deliberate API decision
rather than an implementation gap. Levels are routinely "broken" simply because
someone is still working on them, and a half-carved brush is an ordinary
intermediate state, not an error. So a validation result is a value returned to
a caller who asked for it, and the caller decides what it means. Nothing in
these libraries wakes up and declares your map broken.

## Layout

```text
auto-pigeon-libraries/
├── meta/used-by.schema.json  # schema for each package's consumer manifest
├── workspace/                # the canonical Auto-Pigeon workspace manifest (see workspace/README.md)
├── fixtures/                 # shared apmap fixtures (see fixtures/README.md)
├── games/<game>/             # one folder per per-game data package
├── ts/                       # one folder per TypeScript package
└── go/                       # one folder per Go package (none yet)
```

Every package folder carries its own `README.md` and its own `used-by.json`,
which names the repositories that consume it and validates against
`meta/used-by.schema.json`.

| package | what it is |
| --- | --- |
| [`ts/apmap-schema`](ts/apmap-schema/) | the canonical APMap contract. **1.5 = current READ + WRITE contract; 1.0–1.4 = deprecated legacy READ contracts.** 1.5 adds the Quake III surfaces (bezier `patch` content items and `brush_primitives` texture matrices). Exactly one current schema, the legacy contracts beside it under `schema/deprecated/`, the normative `SEMANTICS.md`, and the conformance vectors. Contract material only, no runtime code. |
| [`ts/health-contract`](ts/health-contract/) | the cross-stack `/version` and `/healthz` contracts every long-lived Auto-Pigeon service answers, and the topology-independent base configuration for the stack's Gatus health monitor. Schemas and one static template, no runtime code. |
| [`ts/incident-contract`](ts/incident-contract/) | the cross-stack incident contract: a **closed** incident envelope (`additionalProperties: false` at every level, `evidence` a fixed set of typed counters), the canonical incident-code taxonomy, the correlation-id convention every component propagates, the central redaction rules, and the versioned user bug-report document (`auto-pigeon-bug-report/1.0`) every report preview, download and submission renders. Contract data plus a dependency-free reference implementation — no transport, no SDK, nothing that runs on its own. |
| [`ts/operational-notice-contract`](ts/operational-notice-contract/) | the operational-notice contract: AUB's closed read response for scheduled-maintenance and other operator notices, the store's text and schedule rules, and the ONE client policy AUP, AUG and AUCOM share — server-time phase, public/authenticated visibility, per-revision dismissal, conditional polling with backoff and a public-only cache — plus IANA-zone/DST vectors for the operator CLI. Contract data plus a dependency-free reference implementation. |
| [`ts/lighting-rules`](ts/lighting-rules/) | the lighting rule-inference contract: the game-independent **analysis** and **held-out validation** artifacts, and the shape of the per-game lighting **vocabulary** they are read through. The separation is the point — geometry is the same in every engine, `light` meaning intensity is not — and a test asserts the generic schemas name no game's classnames. Schemas only, no runtime code. |
| [`ts/offline-workspace-contract`](ts/offline-workspace-contract/) | the cross-stack contract for **Offline Shared Workspaces** — the durable, AUB-authoritative collaboration model that sits beside, and is deliberately not, an AUC Real-time session. Every durable object as one closed schema, the Offline role → capability table under its own version, 58 stable refusal codes each carrying a remedy, nine state machines whose unlisted transitions are illegal by definition, and the product's fixed durations labelled `product` or `contract_default`. Contract data plus a dependency-free reference implementation — no persistence, no endpoint, no UI, no discovery. |

The TypeScript APMap reader/writer/validator is next; it will consume
`apmap-schema` rather than carry its own copy.

## This repository's own version

AULIBS is not a service, so it has no `/version` endpoint to add and none may be added — there is
no server here to serve one from. Its version is the same `1.<commit-count>` every Auto-Pigeon
component uses (`git rev-list --count HEAD`), and it is published the two ways a library can:

- `auto-pigeon-tools` prints it in the VERSIONS manifest every launch emits, and shows it on the
  stack's health dashboard;
- the APMap contract bundle staged into each service image records it in
  `apmap-contract-provenance.json` as `aulibs_version`, beside the `aulibs_commit` it was staged
  from. The commit is the reproducible identifier; the version is the readable one, and neither
  replaces the other.

See [`ts/health-contract`](ts/health-contract/) for the format and the reasoning.

## Running the tests

```bash
./run.sh test
```

It runs every TypeScript package's own `test` script and, once Go packages
exist, `go test ./...`. A package whose dependencies are not installed yet gets
them installed on that first run. With no packages present at all it exits 0 and
says there is nothing to test, rather than failing or printing a green summary
for a suite that never ran.

Some tests read the generated corpora at `$MAPPER_ROOT`, which are not part of
this repository. From a bare clone each skips by name, and everything else runs —
including the whole published contract, current and deprecated.

## The APMap contract, in one sentence

`ts/apmap-schema/schema/` holds **exactly one** `apmap-*.schema.json` — the current contract, the
only format anything writes — and `schema/deprecated/` holds the supported legacy READ contracts
beside it; every service loads the whole directory at startup, derives each version from its
filename, validates a document against the schema matching what that document declares, and refuses
a version the bundle does not hold.

```text
READ        current + supported legacy   (today: 1.0 and 1.1)
WRITE       current only                 (today: 1.1)
VALIDATE    with the schema matching the document
WIRE/COLLAB current only
```

Depth is the mechanism: current discovery reads direct children only, so a deprecated schema can
never become the writer's contract, and `exports` offers no per-version subpath for a producer to
reach for.

## The workspace manifest

[`workspace/auto-pigeon-workspace.json`](workspace/) names the repositories that make up an
Auto-Pigeon workspace, with their aliases, directory names and clone URLs. It is the single
canonical list: `auto-pigeon-tools` resolves workspace topology from it, derives the APMap schema
directory from its AULIBS entry, and its clone and pull scripts derive what to clone and pull from
it rather than each carrying a copy.

Topology only. No machine paths, no `$MAPPER_ROOT`, no ports, no secrets — and no schema path, which
AUT derives rather than reads.

## Using a package

Local-path dependencies are preferred during development; a git-URL dependency
pinned to a commit is the fallback. npm and Go module publication are deferred.

Note that a local-path dependency points outside a consuming repository's Docker
build context, so a consumer that builds an image has to decide how `docker
build` resolves it. See [`AGENTS.md`](AGENTS.md) §5.

## What does not live here: the Auto-Pigeon artwork

The Auto-Pigeon logo and its animations are not in this repository, and a
package for them should not be added here. Everything committed here is
published under Apache-2.0, and no licence has been chosen for the artwork. The
editor repository owns it; the gallery holds one audited, checksum-pinned copy
of the file it shows. Moving it here is a decision for the project owner, not a
side effect of a build task.

## Contributing

[`AGENTS.md`](AGENTS.md) is the authority on how work is done here: the two
product rules, the `used-by.json` protocol, layout, testing, and commit policy.
