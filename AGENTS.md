# AGENTS.md — Auto-Pigeon Libraries (shared libraries)

**This file holds the rules that bind nearly every task here, the source-of-truth order, and a
routing table. Nothing else.** Every detailed rule this repository has lives in exactly one module
under `docs/agent/`, which is READ WHEN A TASK NEEDS IT and is not loaded at startup.

That is a change of shape, not of content. Before `NEW_246G` this file was 427 lines and 3,649
words — already the smallest of the product repositories, and still the used-by protocol, the
layout rules, the consumption mechanism, the clock table and the whole incident contract in every
session, whatever the task was. `docs/agent/section-manifest.json` accounts for every line of that
file and for every one of its 26 headings, and
`../auto-pigeon-tools/scripts/agent_context_router.py gate --repo-root "$PWD"` re-derives the
accounting on every run: a rule that went missing, or a destination that has since been deleted,
fails the gate. **No rule was dropped for looking historical.**

## No questions to HITL between 00:00 and 09:00, Brussels time

Never ask HITL a question between **00:00 and 09:00 Europe/Brussels time** (`TZ=Europe/Brussels
date`), unless HITL has said otherwise: HITL is asleep. A question asked then goes unseen, the
session waits, its context expires, and the prompt has to be stopped and restarted.

In those hours, when you have a question, reason it through and pick what HITL would have chosen in
that situation — from the prompt, earlier HITL decisions in handoffs, `DESIGN.md` and the code — and
keep working. Record in the handoff what you chose, why, and the alternatives, marked for HITL review.

## Do not read or run `run-sequence.sh`

`run-sequence.sh` is the operator's unattended queue drainer. At ~250 KB it is the largest file in
the workspace, it is not your task, and reading it costs the context your task needs.

**Do not read, inspect, verify or debug it, and do not invoke it.** Starting it is the operator's
own action, from their own terminal.

Other files still mention it — as one of the ways a session gets started, or in a design note.
Those mentions are background, not an instruction to go and open it. If `run-sequence.sh` looks
like the cause of whatever you are investigating, say so and stop.

## 1. What this repository is

**auto-pigeon-libraries** is a **public**, Apache-2.0 repository of shared libraries — TypeScript
now, Go later — consumed by the other repositories in this workspace (`auto-pigeon`,
`auto-pigeon-backend`, `auto-pigeon-collaboration`, `auto-pigeon-gallery`, `auto-pigeon-extractor`,
`auto-pigeon-companion`, and any added later), abbreviated **AULIBS**. Its expected location is
`mapper-code/auto-pigeon-libraries/`; the sibling source workspace is `..` and durable mapper data
is under `$MAPPER_ROOT`.

It ships libraries. It is not a service, it has no runtime of its own, and it owns no data.

**It does not hold the Auto-Pigeon brand artwork**, and no task should add a package for it here on
its own initiative. Everything committed here is Apache-2.0 and public, and no licence has been
chosen for the artwork; the editor (`auto-pigeon`) owns it and the gallery holds one
checksum-pinned copy. Decided in `AULIBS/AUP/AUG 236`; revisiting it is the owner's decision,
tracked in the backlog.

Because it is public: **no credentials, no internal hostnames, no references to private
infrastructure in committed content.** The `$MAPPER_ROOT/LLM/` workflow paths referenced in this
file are the workspace convention every sibling `AGENTS.md` already carries, and are the only
non-public paths permitted here.

## 2. The two product rules

These govern everything that will ever live in this repository. They are rules, not preferences.

1. **Validation is on-demand only.** Nothing here may be designed to run automatically, and no
   component ever wakes up and declares a map broken. `docs/agent/aulibs.on-demand-validation.md`.
2. **Every package declares its consumers**, in its own `used-by.json`, and the protocol that makes
   the file worth having has two obligatory halves. `docs/agent/aulibs.used-by.md`.

## 3. Source of truth, in order

When two documents disagree, the earlier one in this list wins.

1. **The active prompt.** It names the work, the execution repository and the complete list of
   repositories you may change.
2. **HITL.** A decision the operator froze, recorded in a handoff or in a module here with its date
   — the artwork decision above is one.
3. **`$MAPPER_ROOT/LLM/WORKFLOW.md`** — the mapper-wide prompt/handoff protocol, shared by every
   repository. This file follows it exactly and does not restate it.
4. **`$MAPPER_ROOT/LLM/DOCTRINE.md`** — the observability doctrine. Binding here in full;
   `docs/agent/aulibs.observability.md` says what it means for a library and
   `aulibs.clock-table` carries the numbers.
5. **The workspace root** — `mapper-code/AGENTS.md` and `mapper-code/CLAUDE.md`, which every
   session here already loads. It is the authority on the cross-repository boundary, where durable
   task material lives, the AUB port contract (9190) and the rule that **component addresses live
   in `.env`, never in code**. Those are not restated here;
   `docs/agent/aulibs.component-addresses.md` explains why the copy that used to be was removed,
   and why a public library's version of the rule is stricter rather than looser.
6. **This file**, for the always-rules and the routes.
7. **The module under `docs/agent/`** that the routing table names for your task. Each one is the
   single authoritative home for its rules, and the gate fails a change that gives one rule two
   homes.
8. **The code and its tests.** When a module and the code disagree, that is a finding to report,
   not a licence to pick one.

`README.md` and each package's own `README.md` are the public, consumer-facing documentation. They
are maintained separately, they are not instruction files, and nothing here may be moved into them.

## 4. The rules that bind every task in this repository

These are unconditional. Everything else is routed.

- **Resolve the task, then checkpoint, then work.** Read `../AGENTS.md`, then the newest handoff in
  `$MAPPER_ROOT/LLM/handoffs/auto-pigeon-libraries/`, then resolve the next prompt from
  `$MAPPER_ROOT/LLM/prompts/auto-pigeon-libraries/`. New prompts are named
  `YYYYMMDD_NN_Title-Case-With-Dashes.md`, with both a `## Prerequisite` prose section and a
  `requires:` frontmatter block (`requires: []` when there is none). `agent_task.py checkpoint`
  targets the *newest* prompt in the queue by default, which is wrong whenever more than one is
  outstanding — pass `--prompt`, or verify the resulting handoff's `prompt_path` before finishing.
- **The handoff is the deliverable, not the terminal.** Create it early with `status: in_progress`,
  refresh it after each coherent phase, and make every checkpoint sufficient for a fresh session to
  continue without replaying the transcript. Handoffs live under `$MAPPER_ROOT/LLM/` and are never
  staged or committed.
- **Print `WORKFLOW.md`'s end-of-task marker as the literal last line of the final response**, at
  every status and however the session was started. This repo's `<ALIAS>` is `AULIBS`, recorded in
  `.agent-repo.json`.
- **`./run.sh test` is the entrypoint and must pass.** A task that ran the tests some other way
  still has to leave `./run.sh test` passing. TypeScript packages run through the package manager's
  own `test` script; Go packages through `go test ./...`, once any Go package exists. With no
  packages present it exits 0 and *reports* that there is nothing to test rather than printing a
  green summary implying a suite ran. **A package with no tests is not accepted** — not "add tests
  later", not "the consumer's tests cover it".
- **Before changing a package, do the consumer audit.** Read its `used-by.json`, inspect every
  listed consumer, grep the siblings for usage the manifest missed, and state in the handoff which
  consumers were checked and how — the commands and their results, not the claim. A change that
  breaks a listed consumer is not forbidden; it is *reportable*.
  `docs/agent/aulibs.used-by.md` is the protocol, both halves.
- **Agents may read and run sibling repositories.** Do not edit, stage, commit, or push a sibling
  unless the active prompt names it as a mutation target. The one sanctioned exception runs the
  other way — a consumer repository adding itself to a package's `used-by.json` — and it covers
  that single file.
- **Commit normally on task completion.** This repository follows the PB/AUC/AUG convention, **not**
  AUP's ask-first rule. One commit per prompt, and only what the library needs to build, test and
  be understood: source, tests, fixtures, package/config files, `used-by.json`, documentation.
  Never `node_modules/`, build output, generated logs, `.env` files or temporary scripts, and never
  `git add .` or `git add -A`.
- **Run-specific and generated material belongs under `$MAPPER_ROOT/LLM/`**, never in this
  checkout.
- **No component may compile in where another component lives** — and a library has nowhere to put
  an address even if it wanted one. `docs/agent/aulibs.component-addresses.md`.
- **Update `README.md` in the same task** when a task adds or materially changes something a
  consumer sees, and remember that a package's own `README.md` is part of what `docs/agent/
  aulibs.layout.md` requires of every package folder.
- **Read the backlog at task start, alongside the prompt** — `$MAPPER_ROOT/LLM/backlog/`, including
  `backlog/workspace.md`, since a cross-repository item is more likely to concern a shared library
  than a single-consumer entry is. `docs/agent/aulibs.backlog.md`.
- **Context lifecycle is not yours.** `run-sequence.sh` owns it through a `PreCompact` hook that
  blocks compaction, checkpoints the work and starts a fresh session on the same prompt. Do not
  estimate your window, adopt a threshold, compact by hand, or stop an unattended run to ask for a
  session reset.
- **Get context from `graft` before grepping or opening source files.** This repository is indexed
  in `graft/`: `graft ask "<task>" --source` to locate and understand, `graft grep "<literal>"`
  when you need EVERY occurrence, `graft callers <symbol>` before renaming anything.
- **A new or changed rule goes in ONE module**, with a link from here if it binds every task. A
  second detailed copy of a rule is a gate failure, and the gate names both files.

## 5. The routing table

Every module is `docs/agent/<id>.md`. Nothing below is loaded at startup: read the one your task
names. `topics` is what `--topic` matches; each module also declares the file globs it governs, and
the router resolves those for you rather than making you read the table.

| module | the authority on | topics |
| --- | --- | --- |
| `aulibs.used-by` | Every package declares its consumers, and the `used-by.json` protocol's two halves | used-by, consumers, protocol, adoption, audit, breaking-change |
| `aulibs.on-demand-validation` | Validation is on-demand only — nothing here wakes up and declares a map broken | validation, on-demand, api-surface, errors, exceptions |
| `aulibs.layout` | Layout — one folder per package, and what each one must contain | layout, packages, folders, fixtures, readme, schema |
| `aulibs.consumption` | How a consumer depends on a package, the unsolved Docker build context, and why there is no Dockerfile | consumption, dependency, npm, go-module, docker, build-context, dockerfile |
| `aulibs.incidents` | Incident observability — AULIBS owns the contract and nothing else | incidents, glitchtip, correlation, redaction, envelope, taxonomy, ajv |
| `aulibs.observability` | The observability doctrine binds, and a library does not log | doctrine, observability, logging, transport, determinism |
| `aulibs.clock-table` | The AULIBS clock table (doctrine pillar 2) | timing, budget, clock, benchmark, measurement, performance |
| `aulibs.component-addresses` | Component addresses live in `.env`, never in code | addresses, env, ports, localhost, port-contract, public-repository |
| `aulibs.backlog` | The backlog — where deferred work lives | backlog, deferral, follow-up, next-task |

## 6. Using the router

```sh
# what should I read for this task? Paths and reasons — never the contents.
../auto-pigeon-tools/scripts/agent_context_router.py --repo-root "$PWD" route \
    --prompt "$MAPPER_ROOT/LLM/prompts/auto-pigeon-libraries/<prompt>.md"
../auto-pigeon-tools/scripts/agent_context_router.py --repo-root "$PWD" route --path ts/incident-contract/schema.json
../auto-pigeon-tools/scripts/agent_context_router.py --repo-root "$PWD" route --topic used-by

# every module, its authority, its topics and the globs it governs
../auto-pigeon-tools/scripts/agent_context_router.py --repo-root "$PWD" modules

# the budget gate, and the accounting for this file's split
../auto-pigeon-tools/scripts/agent_context_router.py --repo-root "$PWD" gate
../auto-pigeon-tools/scripts/agent_context_router.py --repo-root "$PWD" sections | grep '3\.'
```

A prompt may name modules itself with an optional `context_docs:` list in its frontmatter — module
ids or module paths — read by the one frontmatter decoder, so a prompt without it reads exactly as
it did before.

**A package README or a test that cites `AGENTS.md §2.1`, `§3` or `§4D` is still right, and
`sections` is what resolves it.** The module bodies keep their original heading lines verbatim, so
`sections | grep '3\.'` answers *which file* that section now lives in, in one command. Nothing was
renumbered.

The router names files and reasons and **never concatenates them**; its budgets are engineering
budgets over the words an agent loads before anybody types anything. And no `@import` may be added
to `AGENTS.md` or `CLAUDE.md` to "link" a module: `@` is EAGER, so it would put that module's whole
text back in the startup set, and the gate fails it. `auto-pigeon-tools/docs/agent/README.md` is
the authority on the module schema and the route shape; `docs/agent/README.md` here says what is
specific to this repository.
