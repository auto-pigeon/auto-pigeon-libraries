# `@auto-pigeon/apmap-schema`

The canonical JSON Schema for **APMap**, the native JSON geometry document produced by the
Auto-Pigeon extractor (AUE) and shared by Auto-Pigeon, its backend, and the collaboration service.

**Schema and conformance vectors only — no runtime code.** The TypeScript reader, writer, and
validator are a separate package.

```text
1.4 = current READ + WRITE contract
1.3 = deprecated legacy READ contract
1.2 = deprecated legacy READ contract
1.1 = deprecated legacy READ contract
1.0 = deprecated legacy READ contract
```

```text
schema/apmap-1.4.schema.json              THE current contract — exactly one file lives here
schema/deprecated/apmap-1.3.schema.json   a supported LEGACY READ contract — never written
schema/deprecated/apmap-1.2.schema.json   a supported LEGACY READ contract — never written
schema/deprecated/apmap-1.1.schema.json   a supported LEGACY READ contract — never written
schema/deprecated/apmap-1.0.schema.json   a supported LEGACY READ contract — never written
SEMANTICS.md                              the normative specification, SCH-* and SEM-* rules
test-vectors/                             conformance vectors for the CURRENT contract
deprecated/1.0/                           the published 1.0 corpus and examples
deprecated/1.1/                           the published 1.1 corpus
deprecated/1.2/                           the published 1.2 corpus
deprecated/1.3/                           the published 1.3 corpus
workspace/ (repository root)              the canonical workspace manifest
```

## The runtime doctrine

```text
READ        current + every supported legacy format
WRITE       current only
VALIDATE    with the schema matching the document being read
WIRE/COLLAB current only
```

Today that resolves to: read 1.0, 1.1, 1.2, 1.3 and 1.4, write 1.4.

**It is not a compatibility matrix.** Nothing here, and nothing in any consumer, maintains a list of
supported versions. The directory layout IS the policy, and every service derives both halves of it
by reading the directory at startup:

```text
the ONE direct schema/apmap-*.schema.json   CURRENT — the only format anything writes
each schema/deprecated/apmap-*.schema.json  LEGACY  — readable, never written
```

Depth is the whole mechanism. The current-schema scan reads **direct children only**, so a
deprecated contract can never be mistaken for the writer's; a reader that wants backward
compatibility enumerates `deprecated/` deliberately. Promoting a version is one file rename and one
file move — plus repointing this package's `exports`, which is the one place a filename is still
typed and which `contract-integrity.test.mjs` fails on if it is forgotten.

`deprecated` means **"not current / never written"**, not "unreadable". A format that becomes
genuinely unreadable does not stay here — it moves out of the readable tree into an explicitly
unsupported archive, and that decision has not been taken for any version.

A document declaring a version this bundle does not hold is refused at the version gate, before
schema validation is reached. Unknown future versions are still refused; that has not changed.

The current schema's `apmap_version` enum still contains `1.0`, so it would *structurally* accept a
1.0-shaped document. **That is not how a 1.0 document gets read.** A reader picks the schema
matching the declared version, so a 1.0 document is validated against the frozen 1.0 contract and
never against this one. `contract-integrity.test.mjs` asserts it, precisely so that nobody later
"fixes" the enum believing it is the mechanism behind either rule.

## Reading a legacy document, and why nothing writes one

`schema/deprecated/` ships with the package (`files` lists `schema`, which carries the subdirectory
with it) because a runtime reader consumes the contract **directory**, not a single file. A
published tarball missing the legacy contract would be a build that cannot open the maps its own
users already have.

What stays closed is the *writer's* door. There is no `/1.0` export, no per-version export subpath,
and no package API that lets a producer select a contract: `exports` resolves to the current schema
and nothing else, and `contract-integrity.test.mjs` fails if a versioned subpath ever appears. A
consumer that can choose a version is a consumer that will eventually choose the wrong one.

`deprecated/1.0/` — the published corpus and examples, as distinct from the contract — is historical
evidence for this repository's own tests. It is not shipped and is not a runtime input.
`deprecated/1.0/README.md` is the full account.

## Every version is additive, which is what makes migration cheap

Each version was built **additively** from the one before, and `test/schema.test.mjs` walks the
documents and fails if that stops being true. 1.1 added one `derived_from` kind (`operation`) and
one optional brush member (`broken`). 1.2 added `groups`. 1.3 added one optional group member,
`source`. 1.4 adds the optional document member `authorship` and a fourth group-member kind,
`group` — and makes the one **relaxation** in the history: a persisted group needs one member,
not two. The walk from 1.3 to 1.4 allows exactly that difference and nothing else.

That matters for exactly one reason: **promotion is mechanical**. A legacy document becomes a
current one by rewriting the declared version and supplying the structural defaults the current
contract requires — today exactly one, `groups: []`. No geometry is rebuilt, no id reminted, no
provenance rewritten, and nothing is invented: 1.3's `source` is optional, so a promoted group
carries no origin because it never had one recorded; 1.4's `authorship` is optional, so a promoted
document names no author, copyright holder or licence it never declared. A legacy document is
validated against its **own** frozen contract before it is promoted, so a 1.3 file with a
one-member group is refused as the invalid 1.3 document it is, not laundered into valid 1.4.
`test/helpers.mjs` holds that promotion in one place as `promoteToCurrent`;
`test/deprecated-history.test.mjs` proves it over the whole published 1.0 corpus,
`test/promotion.test.mjs` over every deprecated corpus (idempotence included), and
`test/corpus.test.mjs` over the generated corpora when they are mounted. That is what lets a
consumer open a legacy document, promote it in memory, and write current bytes without a migration
wizard or a lossy conversion.

## What 1.4 adds

### `authorship` — who made the document

```json
{
  "authorship": {
    "author": "Zoë O'Neil & Bob",
    "copyright_notice": "© 2026 Zoë O'Neil.\nAll rights reserved.",
    "license": "CC-BY-SA-4.0"
  }
}
```

Optional, closed, and every member optional. **Absent means unknown, `""` means deliberately
blank**, and anything else is the author's text verbatim — never trimmed, normalized or
re-punctuated. `author` is one line (≤ 256); the notice (≤ 1024) and the licence (≤ 4096) may span
lines with LF. It is attribution, not identity: the uploader or signed-in account is never written
into `author`, and the closed object has no `uploader` member to put it in. `license` is text
somebody typed, never permission any component may act on. Richer data belongs in a namespaced
`extensions` entry. `SEMANTICS.md` §9b is the normative account, including what a derived prefab
keeps.

### Nested groups, and a group of one

```json
{ "group_id": "grp_entrance00001", "name": "Entrance",
  "members": [ { "kind": "group", "group_id": "grp_columns0000001" },
               { "kind": "brush", "brush_id": "brs_cube00000000c3" } ] }
```

A member may now be another group. One parent per group, no cycles, at most 32 levels, and
containment normalization across a whole tree; expanding a group reaches each object once, so a
transform moves each brush once. A persisted group needs **one** member — a CUT may leave a real
group holding a single brush — and an empty one is still refused. Creating a group by hand may still
require two selected items; that is the editor's rule, not the document's. `SEMANTICS.md` §9a has
the before/after graph for delete, Ungroup, reparent and copy/paste.

Some of these rules are about the whole document and cannot be said in JSON Schema. The new
`test-vectors/semantic-invalid/` set holds one schema-valid document per such rule, and
`index.json`'s `semantic_invalid` list names the rule a conforming validator must report for each.

## What 1.3 added

### `group.source` — where a group came from

```json
{
  "group_id": "grp_entrance00001",
  "name": "Entrance Columns",
  "members": [ ],
  "source": { "kind": "prefab", "prefab_id": "user/qk3m2p9x7v1a0zt/9f2c41ab7d0e6538" }
}
```

Optional, and **historical origin rather than a live binding**. It records that this group was
created by placing that prefab. It does not say the geometry still matches it, it does not track
later edits on either side, and deleting the prefab from a library neither deletes the group nor
falsifies the record. So an ordinary edit — rename, add a member, move the geometry — keeps it, and
copying the whole group keeps it too under a fresh `group_id`, because the copy has the same
history. A group made any other way simply omits the member.

**Only the identity is stored.** No screenshot bytes, no image URL, no host, no title, no owner, no
revision, no extraction job. Those belong to the prefab library, which owns the asset, can revoke
access to it and can re-render it; a copy in the map would be a stale, unauthenticated second
source of truth. A consumer resolves the picture from `prefab_id` at runtime, and when it cannot —
the prefab was deleted, or lives in an account this reader cannot see — the provenance still stands
and the picture is simply absent. `additionalProperties: false` is what keeps the asset out, and
`test/schema.test.mjs` names the members it keeps out so a later "just one more field" has to argue
with a test.

`prefab_id` is an **external** reference: the library's identity, not a map object id. It carries
no APMap prefix, nothing in the document declares it, and it is never reminted when the document's
own ids are — a clone of a sourced group gets a new `group_id` and the *same* `prefab_id`. AUB's
user prefabs are `user/<owner>/<digest>`, slashes included, which is why the contract bounds the
string and imposes no pattern on it.

## What 1.2 added

### `groups` — named, persistent selection sets

A group is a name over a set of objects the document already declares:

```json
{
  "groups": [
    {
      "group_id": "grp_entrance00001",
      "name": "Entrance Columns",
      "members": [
        { "kind": "brush",  "brush_id": "brs_cube00000000a1" },
        { "kind": "entity", "entity_id": "ent_light000000001" }
      ]
    }
  ]
}
```

It owns no geometry, so it is cheap: deleting a group deletes nothing but the grouping. It is
**map working state**, which is the whole reason it is here rather than in a sidecar — it is saved
with the document, undone with the document, and seen by every collaborator, and none of those are
true of a record kept beside the map. That argument is also what produced 1.3: `group.source` lived
in browser memory for exactly one release, and it did not survive a reload.

`groups` is **required of a current writer**, empty array included. An optional member would make
"this map has no groups" and "this producer has never heard of groups" the same bytes.

`SEMANTICS.md` §9a is the normative account: the discriminated member union, the member
minimum (two through 1.3, one from 1.4), containment normalization, `source`, and the SCH-G-*/SEM-G-* rules.

## What 1.1 added

### `derived_from` kind `operation`

Provenance that stays truthful after an edit.

In 1.0 an object's `derived_from` says where it came from — `source_map`, `package`, `authored`, or
`synthetic`. The moment an operation rewrites an object, a `source_map` record becomes a false
statement: the geometry no longer matches the named location in the named file. The `operation` kind
replaces it with something still true — *this object exists because operation X replaced its
ancestor.*

```json
{
  "kind": "operation",
  "operation_id": "op-4f1c8a20",
  "operation": "csg_carve",
  "ordinal": 0,
  "replaces": ["brs_ancestor00000a"],
  "intent": "carve a doorway through the north wall"
}
```

`kind` and `operation_id` are the whole of what is required; everything else is optional.

**It is not the same as `authored`,** which 1.0 already had and which also carries an `operation_id`.
`authored` records an object an operation *created*, and its identity is derived from
`document_id` + `operation_id` + `ordinal`. `operation` records an object that *supersedes* one that
was already there, and names what it superseded in `replaces`. A carve produces both kinds of
history and the two are not interchangeable.

**`intent` and `metadata` are annotation, never authorization.** Nothing may read either field to
decide whether an operation was permitted, what it was allowed to touch, or how to re-apply it. They
are written by whoever ran the operation, they are checked against nothing, and a document is not a
trusted channel. They exist so that a human reading provenance months later can tell why the geometry
changed. `additionalProperties` is `false` on the kind, so a producer cannot introduce a field a
consumer might mistake for permission — one of the rejection vectors is exactly that attempt.

### `broken` on a brush

```json
{ "kind": "brush", "brush_id": "brs_...", "faces": [...], "broken": true }
```

Optional boolean; absent means false, and there is no third state. It records that a brush is known
not to be a well-formed convex solid **and is being kept anyway** — CSG repair can fail, and the user
may decide the result is what they want for now.

It crosses the wire in collaborative sessions and survives Save. Export to `.map` warns and exports
anyway; that behaviour belongs to exporters rather than to this schema, and the schema says so in the
field's description so implementers inherit one intent instead of each inventing one.

Consistent with the repository's first product rule, the flag is not a validation result and this
schema never requires it to be present on a brush that is in fact broken. Nothing here validates
geometry on its own, and nothing may refuse to load, silently repair, or drop a brush because the
flag is set. A level in that condition is ordinary work in progress.

## Using it

Writing, or validating something you are about to write — the package default, and the only
contract a producer may ever hold:

```js
import Ajv2020 from 'ajv/dist/2020.js';
import schema from '@auto-pigeon/apmap-schema' with { type: 'json' };

const validate = new Ajv2020({ allErrors: true, strict: false }).compile(schema);
if (!validate(document)) console.log(validate.errors);   // when you ask, and only then
```

`@auto-pigeon/apmap-schema` and `@auto-pigeon/apmap-schema/schema` both resolve to the current
schema. There is no `/1.0` subpath and no version argument.

Reading, where a legacy document may arrive: take the schema **directory** and load the bundle, the
way a service does at startup. `test/helpers.mjs`'s `loadContractBundle` is the reference shape —
one current schema, every `deprecated/apmap-*.schema.json` beside it, keyed by the version each
filename carries — and every fault in it is a startup failure, including a broken legacy contract
nothing has asked for yet.

`ajv/dist/2020` is the reference validator because it is what Auto-Pigeon's own APMap pipeline
compiles this schema with. Any 2020-12 validator will do.

In a browser whose Content-Security-Policy has no `unsafe-eval`, `compile()` throws: `ajv` turns the
schema into JavaScript and evaluates it. Do that at build time instead — `ajv/dist/standalone` over
these same files, with the same options — and ship the generated code; match it to the schema a
backend serves by a digest of the schema itself, never by the version label alone. Auto-Pigeon's
editor does exactly that (`NEW_267`, see `used-by.json`); the files here are unchanged by it.

## What the schema cannot say

JSON Schema covers structure. The APMap rules numbered `SEM-*` — one id per object, relationship
endpoints that exist, a brush that encloses a finite volume, plane points that are not collinear,
numbers that are finite — are not expressible in it and are a validator's job. Seven of the published
1.0 rejection vectors pass this schema for exactly that reason; that is the design, not a gap.
`SEMANTICS.md` in this package is the rule catalogue.

## Tests

```bash
npm test            # from this directory
./run.sh test       # from the repository root, with every other package
```

The corpus tests need `$MAPPER_ROOT`; from a bare public clone they skip and say so, and the rest
still runs.
