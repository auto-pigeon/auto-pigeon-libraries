<!--
  CANONICAL LOCATION: auto-pigeon-libraries/ts/apmap-schema/SEMANTICS.md

  The normative APMap specification: document shape, identity, coordinate frame, canonical
  serialization, and — §10 and §11 — the split between what JSON Schema enforces (SCH-*) and what a
  consumer must enforce in code (SEM-*).

  THIS IS CURRENT CONTRACT MATERIAL, not history. The SEM-* rules are what every consumer's semantic
  checker implements, and the current contract inherits them unchanged. The title says 1.0 because
  that is the version the document was written for and 1.0 is frozen; 1.1 adds one `derived_from`
  kind and one optional brush member and changes nothing here; 1.2 adds `groups`, specified in §9a
  with its own SCH-G-* and SEM-G-* rules; 1.3 adds the optional `group.source`, specified in the
  same section; 1.4 adds the optional document member `authorship` (§9b, SCH-A-*/SEM-A-*), lets a
  group contain another group, and lowers a persisted group's minimum from two members to one (§9a,
  SCH-G-10..11, SEM-G-8..9 and the revised SEM-G-4/5/6); 1.5 adds the Quake III surfaces — the
  entity content item `patch`, the face projection `brush_primitives` and the group member kind
  `patch` — with their own section (§9c), SCH-Q-*/SEM-Q-* rules and serialization rule SER-7Q.

  It was published at $MAPPER_ROOT/formats/apmap/1.0/README.md until this repository became the
  authority, so that a public clone can read the normative rule catalogue without a data root.

  The DEPRECATED 1.0 material — the frozen schema, its conformance vectors and its examples — is
  under schema/deprecated/ and deprecated/1.0/. None of it is a runtime surface.
-->

# APMap 1.0

APMap is the native JSON geometry-document format produced by the **Auto-Pigeon
extractor** (AUE) and shared by **Auto-Pigeon** (AUP), its backend (AUB), and the
collaboration service (AUC).

One `.apmap` file is **one geometry document or one prefab geometry layer**. It
carries geometry, stable identity, coordinate-frame information, the object
metadata needed to interpret that geometry, and the references required by
geometry relationships.

It does **not** carry package classification, behaviour, validation results,
recipes, or review evidence. Those stay in their existing sidecars
(`prefab.json`, `classification.json`, `provenance.json`, `validation.json`,
`recipe.json`).

| file | content |
| --- | --- |
| `apmap.schema.json` | JSON Schema 2020-12, `$id` `https://auto-pigeon.org/schemas/apmap/1.0/apmap.schema.json` |
| `examples/minimal.apmap` | the smallest legal document |
| `examples/prefab-core.apmap` | a real extracted Quake 1 `func_door` prefab core |
| `test-vectors/valid/` | 10 documents that must be accepted |
| `test-vectors/invalid/` | 21 documents that must be rejected, each naming its rule |

---

## 1. Package layout

A prefab package uses **same-stem geometry siblings**. The `.map` files remain
exactly as they are; the `.apmap` files are added beside them.

```text
prefab.map                 prefab.apmap             role: prefab_core
context.map                context.apmap            role: reference_context
context-cropped.map        context-cropped.apmap    role: cropped_context
```

A standalone map uses `role: full_map`.

---

## 2. Document shape

```json
{
  "$schema": "https://auto-pigeon.org/schemas/apmap/1.0/apmap.schema.json",
  "apmap_version": "1.0",
  "document_id": "01jqapmapprefabcoredoor01",
  "revision": 0,
  "role": "prefab_core",
  "game": "quake1",
  "map_dialect": "quake_standard",
  "id_policy": "derived",
  "frame": { },
  "provenance": { },
  "authorship": { },
  "entities": [ ],
  "relationships": [ ],
  "extensions": { }
}
```

`$schema`, `provenance`, `authorship` (1.4, §9b), `relationships` and
`extensions` are optional. `groups` (1.2, §9a — omitted from the 1.0 sketch
above) is required of every 1.2+ writer and sits between `entities` and
`relationships`. Every other member is required. The member order above is the
canonical order (rule SER-5).

### `role`

| role | meaning | insertable |
| --- | --- | --- |
| `full_map` | a complete map document | — |
| `prefab_core` | the insertable prefab geometry | yes |
| `reference_context` | the host geometry around the core, review evidence | no |
| `cropped_context` | context clipped to a box around the core, display-only | no |

`cropped_context` documents contain invented cap faces (§7) and must never be
committed into an active map.

### `game` and `map_dialect`

APMap 1.0 normatively specifies **Quake 1 only**, with both classic and Valve
220 face syntax. Both members are constrained strings, and these names are
reserved so a later version adds semantics rather than fields:

```text
game:        quake1  quake2  quake3  half_life
map_dialect: quake_standard  valve_220  quake2_extended  quake3_extended  vmf
```

A 1.0 consumer that does not implement a reserved name must reject the document
rather than guess. Quake 2-style trailing numeric face values still round-trip
verbatim in `face.tail` (§6.3) because the MAP parser carries them; 1.0
does not interpret them.

**1.5 gives `quake3` + `quake3_extended` their meaning**: the Quake III `.map`
grammar that Q3Map2 compiles — classic faces with the three-number tail,
brushDef faces (§6.2a) and patchDef2 patches (§9c). It is also the only
combination in which a `patch` or a `brush_primitives` face may appear
(SEM-Q-4). What a *consumer* does with it is that consumer's decision, stated
where it refuses: a component that cannot yet draw, edit or compile a Quake III
surface refuses the document by name and never flattens, drops or converts
what it does not implement.

---

## 3. Identity

Keep four concepts separate.

| field | meaning |
| --- | --- |
| `apmap_version` | file-format and schema version |
| `document_id` | permanent identity of this independent document |
| `revision` | durable revision number of the document |
| `entity_id` / `brush_id` / `face_id` | stable identity **within** the document |

### 3.1 Complete identity is `(document_id, object_id)`

```json
{ "document_id": "document-B", "object_id": "brs_1c77e0a942fb5d38" }
```

No API, lease, operation, or database record may treat `brs_1c77e0a942fb5d38`
alone as globally unique.

### 3.2 Cloning

A clone receives a **new `document_id`** and initially **preserves every
internal object id**:

```text
Original: document-A / brs_1c77e0a942fb5d38
Clone:    document-B / brs_1c77e0a942fb5d38
```

These are different objects, because `(document-A, X) ≠ (document-B, X)`.
Preserving internal ids avoids rewriting references, constraints, prefab
relationships, and annotations; the two documents are independent immediately
because every operation and lease is scoped by `document_id`.

### 3.3 Object id syntax

```text
entity  ent_<16 hex>       ^ent_[0-9A-Za-z]{8,64}$
brush   brs_<16 hex>       ^brs_[0-9A-Za-z]{8,64}$
face    fac_<16 hex>       ^fac_[0-9A-Za-z]{8,64}$
patch   pat_<16 hex>       ^pat_[0-9A-Za-z]{8,64}$     (1.5)
```

A patch id is not an `object_id` (§8): a relationship cannot name a patch in 1.5,
and the `synthetic.source_object` pointer cannot either. Adding either is a
later, additive decision.

The typed prefix makes a relationship endpoint self-describing and makes a
brush id used where an entity id belongs a schema error, not a silent bug.

### 3.4 `id_policy`

| policy | contract |
| --- | --- |
| `derived` | every object carries `derived_from`, and its id **equals** the digest of that record's identity inputs. Verifiable and reproducible. |
| `minted` | ids are opaque and producer-assigned. `derived_from`, if present, is provenance only and is not checked. |

AUE emits `derived`, so regenerating a package reproduces byte-identical ids.

### 3.5 The derivation algorithm

```text
US       = U+001F (unit separator)
payload  = "apmap/1.0" US kind US c1 US c2 US ... US cN
digest   = lowercase_hex( SHA-256( UTF-8(payload) ) )[0:16]
id       = prefix(kind) + "_" + digest
```

`kind` is `entity`, `brush`, `face` or — from 1.5 — `patch`, and is part of the
payload, so an entity and the brush at the same source path can never collide. `US` cannot occur in a
component. SHA-256 is mandatory — never a language's built-in `hash()`, which is
randomized per process.

**Components by `derived_from.kind`:**

| kind | components |
| --- | --- |
| `source_map` | `source_map`, `source_sha256`, `source_path` |
| `package` | `package_id`, `role`, `object_path` |
| `authored` | `document_id`, `operation_id`, `ordinal` (decimal, no padding) |
| `synthetic` | none mandated in 1.0; the producer assigns the id |

`source_path` addresses the object inside the **source** map; `object_path`
addresses it inside **this** document. Both use the same grammar:

```text
e30            entity 30
e30.b0         its first brush
e30.b0.f0      that brush's first face
e30.p0         its first patch (1.5)
```

`b<n>` counts an entity's brushes and `p<n>` its patches, each from 0 and each in
content order, so adding a patch never renumbers a brush. A patch has no
sub-objects: there is no `e30.p0.f0`. The grammar was widened by `.p<n>` in 1.5
(§13); every path 1.4 accepted means what it meant.

Use `source_map` when the producer knows the exact source object and the source
file's hash. Use `package` when the stable semantic identity is the published
package rather than a source file — an extraction package whose layers are
regenerated as a unit, where the source object for a host context brush is not
individually tracked. `role` is part of the digest, so the same `object_path` in
`prefab.apmap` and `context.apmap` yields **different** ids: those two documents
describe different objects at that path, and a matching id would invite a false
correlation.

### 3.6 What identity must **not** depend on

The digest inputs are exactly the members listed above. Everything else in
`derived_from` — `source_entity_index`, `source_brush_position`,
`origin_at_extraction`, `bounds_at_extraction` — is **provenance**, recorded for
reconstruction and debugging, and never fed back into identity.

This is deliberate and load-bearing. Coordinates are recorded but excluded, so
that a live operation such as

> move `brs_1c77e0a942fb5d38` 32 units left

leaves the object's identity intact. An id derived from geometry would change on
every move and every journal reference would dangle.

Ids therefore remain stable when geometry, texture, metadata, or array position
changes, and are never derived from output array position, parser order of the
output document, geometry hashes, or texture values.

**A source-derived id does change when the source map changes**, because
`source_sha256` is an input. That is correct: re-extracting from a modified
`e1m4.map` genuinely yields different objects, and silently reusing the old ids
would assert an equivalence nobody verified.

### 3.7 Objects with no source

Geometry a user draws in the browser has no source object. Its identity comes
from the **operation journal**, which is itself deterministic:

```json
{
  "brush_id": "brs_7ae30f19c5d2b84a",
  "derived_from": {
    "kind": "authored",
    "document_id": "01jq...",
    "operation_id": "op-9f2a",
    "ordinal": 1
  }
}
```

Replaying the same journal against the same document reproduces every id
exactly, so a recovered WIP checkpoint and a replayed session agree.

`ordinal` counts objects created by **one** operation, starting at 0. One
operation that creates an entity, its brush, and six faces uses ordinals 0..7.

---

## 4. Coordinate frame

Every document declares its own space and the transform back to source world, so
**a consumer never reads a sidecar to place geometry**.

```json
"frame": {
  "space": "prefab_local",
  "units": "quake_unit",
  "coordinate_system": { "handedness": "right", "up_axis": "z" },
  "to_source": { "type": "translation", "translation": [1224, 999, 1384] }
}
```

`to_source` maps this document's coordinates **into** source-world coordinates.
Applying it to a `prefab_local` point yields the point's original position in
the source map.

| space | meaning |
| --- | --- |
| `source_world` | the source map's own coordinates; `to_source` must be `identity` |
| `prefab_local` | canonicalized prefab coordinates; `to_source` carries the translation |

This solves a real problem. In today's packages `prefab.map` is prefab-local
(`( -112 9 192 )`) while its sibling `context.map` is source-world
(`( 1056 752 1384 )`), reconciled only by `provenance.json`. AUP currently
searches five candidate metadata key paths for that transform and withholds the
whole context layer when none parse. With APMap each document answers for
itself.

**1.0 permits `identity` and `translation` only.** The name `affine` is reserved
and is rejected by the 1.0 schema. Every artifact that exists today is a pure
translation, and AUP's renderer already refuses rotated transforms, so allowing
a matrix nobody emits would only create documents that validate and cannot be
drawn.

`units` is `quake_unit`. `coordinate_system` is optional and, when present, must
be right-handed and Z-up.

---

## 5. Entities and ordered content

An entity is an **ordered** list of content items. Repeated keys are legal and
their position relative to brushes is significant, so a `.map` entity
round-trips exactly.

```json
{
  "entity_id": "ent_432a6b642793e26b",
  "derived_from": { "kind": "source_map", "source_path": "e30", "...": "..." },
  "content": [
    { "kind": "property", "key": "classname", "value": "func_door" },
    { "kind": "property", "key": "angle", "value": "180" },
    { "kind": "brush", "brush_id": "brs_...", "faces": [ ] },
    { "kind": "property", "key": "message", "value": "written after the brush" }
  ]
}
```

This mirrors the MAP parser's model exactly, where an entity is a sequence of
`KeyValue | Brush`. A representation with a separate properties map and brushes
array could not round-trip either a repeated key or the ordering above.

An entity may legally carry **no brush at all** — that is a point entity. Keys
and values are MAP strings and cannot contain a double quote, carriage return,
or line feed, because the MAP writer cannot quote them.

From 1.5 an item may also be a **patch** (§9c). It takes its position among the
entity's items exactly as the source wrote it — a patch between two brushes stays
between them — and a writer never sorts patches after brushes or properties.

---

## 6. Brushes and faces

A brush is the intersection of half-spaces, one per face — not a bounded mesh.
It needs at least four faces and must enclose a finite, non-degenerate volume.

```json
{
  "kind": "brush",
  "brush_id": "brs_1c77e0a942fb5d38",
  "derived_from": { },
  "faces": [ ]
}
```

### 6.1 Face plane

Exactly three points, in MAP order. The plane normal and the solid side follow
the source dialect's winding convention; APMap stores the points rather than a
normal-and-distance so nothing is lost to floating-point renormalization.

```json
"plane": { "points": [[-112, 9, 192], [-112, -7, 192], [-112, -7, 176]] }
```

### 6.2 Texture projection

Exactly one of two shapes. Mixing members from both is a schema error, because
one brush cannot mix face syntaxes in a `.map` file.

```json
"projection": { "mode": "classic",  "shift": [999, -1384], "rotation": 0, "scale": [1, 1] }
```

```json
"projection": {
  "mode": "valve220",
  "u_axis": [1, 0, 0, 0],
  "v_axis": [0, -1, 0, 0],
  "rotation": 0,
  "scale": [1, 1]
}
```

`u_axis` and `v_axis` are `[x, y, z, shift]`.

### 6.2a Brush primitives (1.5)

A Quake III brushDef face carries a 2×3 texture matrix instead of shift, rotation
and scale. 1.5 stores it **verbatim** as a third projection shape:

```json
"projection": { "mode": "brush_primitives", "matrix": [[0.0078125, 0, 0], [0, 0.0078125, 0]] }
```

A point `p` on the face gets texture coordinates, in **texture units** (1.0 is
one repeat of the image, whatever its pixel size):

```text
x' = dot(p, texS)        y' = dot(p, texT)
s  = m00·x' + m01·y' + m02
t  = m10·x' + m11·y' + m12
```

`texS` and `texT` are the axis base Q3Map2 derives from the face normal `n`
(computed from the three plane points as Q3Map2 does,
`normalize((p0 − p1) × (p2 − p1))`), after zeroing any component of `n` whose
magnitude is below 1e-6:

```text
rotY = −atan2(n.z, sqrt(n.x² + n.y²))      rotZ = atan2(n.y, n.x)
texS = (−sin rotZ,            cos rotZ,            0)
texT = (−sin rotY · cos rotZ, −sin rotY · sin rotZ, −cos rotY)
```

**Why not convert it to `classic`.** It was measured before the shape was
chosen (`test/quake3.test.mjs`): a classic projection converts to a matrix and
back with an error below 1e-9 on rotated, mirrored and sloped faces — **but only
given the image's pixel size**, which the document does not hold, and a matrix
with shear has no classic form at any size (a re-derived matrix lands more than
1e-3 away). A conversion would therefore be either impossible or dependent on
data outside the document, so the matrix is the source of truth and is never
rewritten by a reader. The matrix must be non-degenerate (SEM-Q-3), and a brush's
faces are all brush primitives or none are (SEM-Q-5); Q3Map2 itself refuses a
`.map` that mixes brushDef brushes with classic ones, so a Quake III `.map`
exporter converts or refuses — the document does not decide that for it.

**Precision.** The coefficients Radiant writes carry six *significant* digits,
not six decimal places, so SER-7's rounding would change them. They follow
SER-7Q (§12).
### 6.3 Face tail

Quake 2-style trailing numeric face values are preserved verbatim and
uninterpreted in 1.0:

```json
"tail": [1, 0, 0]
```

Absent when the source face had none. The MAP parser reads and writes
these, so they survive a round trip without 1.0 assigning them meaning.

A Quake III face — classic or brushDef — ends in three such numbers (content
flags, surface flags, value). They are stored in `tail` exactly like Quake 2's,
and 1.5 assigns them no meaning either: a shader's own `surfaceparm`s, not the
map, are where Quake III usually says what a surface is.

---

## 7. Synthetic faces

A face invented by a generator — a context-crop cap — declares itself:

```json
{
  "face_id": "fac_...",
  "derived_from": {
    "kind": "synthetic",
    "generator": "auto-pigeon-extractor.context_crop",
    "reason": "context_crop"
  },
  "plane": { "points": [ ] },
  "texture": "CONTEXT_CROP",
  "projection": { },
  "synthetic": true,
  "synthetic_reason": "context_crop"
}
```

`synthetic` and `synthetic_reason` appear together or not at all (SEM-3). This
replaces the need to cross-reference `context-cropped.meta.json` by
`entity_index`/`brush_index`/`face_index` to learn whether a face is authored;
the sidecar remains the record of the crop operation, but the geometry document
is now self-describing.

---

## 8. Relationships

One typed edge shape, referencing stable object ids:

```json
"relationships": [
  {
    "type": "activation_source",
    "from": "ent_a41f...",
    "to": "ent_9f2a...",
    "payload": { "via": "target", "key": "t1" }
  }
]
```

The registry is seeded only with relations AUE actually emits today:

| type | meaning | evidence |
| --- | --- | --- |
| `entity_target` | `target` → `targetname` edge | AUE `entitygraph` |
| `assembly_member` | members of one extracted assembly | `provenance.assembly_id`, e.g. `e1m4:e30+e31` |
| `activation_source` | what triggers an entity | `activation.json` raw_sources |

`type` is an open constrained string, so a consumer may introduce its own kind
without a schema bump. An unknown type validates structurally and must be
**preserved**, not dropped. Both endpoints must reference objects declared in
the same document (SEM-2).

---

## 9. Extensions

Namespaced vendor data, the only open bag in the format:

```json
"extensions": {
  "auto-pigeon-extractor.context_crop": { "display_only": true, "margin": [128, 128, 128] }
}
```

A namespace must contain at least one dot, so no producer can claim a bare word.
Extensions are allowed on the document, on an entity, on a brush, and on a face.
Values must be objects. A consumer must preserve extensions it does not
understand.

---

## 9a. Groups

A **group** is a named, persistent selection set. It is map working state:
saved with the document, undone with the document, and seen by every
collaborator.

```json
{
  "groups": [
    {
      "group_id": "grp_entrance00001",
      "name": "Entrance Columns",
      "members": [
        { "kind": "brush",  "brush_id": "brs_cube00000000a1" },
        { "kind": "entity", "entity_id": "ent_light000000001" },
        { "kind": "face",   "face_id": "fac_00cube0000" }
      ]
    }
  ]
}
```

A group **owns no geometry**. Every member is a reference to an object the
document already declares elsewhere — or, from 1.4, to another group — so
deleting a group deletes nothing but the grouping. A group owns **selection and
organization**: what is picked together, what is moved together, what is named
together. It never owns brush geometry, and nothing about a group changes a
compiled world.

`groups` is **required of a 1.2 writer**, empty array included. An optional
member would make "this map has no groups" and "this producer has never heard
of groups" the same bytes, and a tool that drops what it does not understand
would be indistinguishable from one that faithfully recorded a map with no
groups.

### `group_id`

Stable canonical identity, `grp_` prefixed, minted like any other object id and
unique across the document. It is **never derived from the name**: a name is a
label a user retypes, and identity that moves when a label is edited is not
identity.

### `name`

The user-visible label: 1–128 characters, at least one of them non-whitespace.
It is a **label, not a reference** — nothing may address a group by name, and
two groups in one document may carry the same name.

### `members`

A discriminated union on `kind`, each arm naming the id field for that kind.
One untyped `object_id` would let a brush id sit under an entity member and fail
later, at resolution, rather than at the contract.

| kind | id field | since |
| --- | --- | --- |
| `entity` | `entity_id` | 1.2 |
| `brush` | `brush_id` | 1.2 |
| `face` | `face_id` | 1.2 |
| `group` | `group_id` | 1.4 |

The membership rules, by version:

| rule | 1.2 – 1.3 | 1.4 |
| --- | --- | --- |
| minimum direct members | **two** | **one** |
| empty group | invalid | invalid |
| duplicates in one member list | invalid | invalid |
| an object is a direct member of | at most one group | at most one group |
| a group is a direct member of | — (no nesting) | at most one group: **one parent** |
| cycles | — | invalid, of any length, self-links included |
| nesting depth | — | at most **32** groups from a root to its deepest descendant |

**Why one.** A CUT splits a group's geometry into a cut-out and a remainder, and
either side may hold a single brush. Under 1.3 the only ways to keep that result
grouped were to lose the group or to invent a sliver of geometry to reach a
count, and the second corrupts the map to satisfy a counter. A persisted group
of one is therefore valid from 1.4. An **empty** group is not: the editor
dissolves a group in the same transaction that removed its last member, and a
parent emptied by that dissolution dissolves with it, bottom-up, in the same
transaction.

**Validity is not the creation gesture.** Whether an editor lets a user create a
group by hand from a single selected item (Ctrl+G) is an editor rule, not this
one; an editor may keep asking for two. The document rule says only what a
saved group may be.

**A 1.3 document is still held to 1.3.** A file declaring 1.3 is validated
against the frozen 1.3 contract, where two members remain the minimum and the
`group` arm does not exist, *before* it is promoted. A one-member group declared
as 1.3 is invalid, and promotion never launders it into a valid 1.4 document.

### Nesting

A `group` member makes the named group a **child**. The graph of groups is a
forest: each group has at most one parent, no cycle exists, and every group is
reachable from exactly one root (a group no other group lists).

```json
{ "group_id": "grp_columns0000001", "name": "Columns",
  "members": [ { "kind": "brush", "brush_id": "brs_cube00000000a1" },
               { "kind": "brush", "brush_id": "brs_cube00000000b2" } ] },
{ "group_id": "grp_entrance00001", "name": "Entrance",
  "members": [ { "kind": "group", "group_id": "grp_columns0000001" },
               { "kind": "brush", "brush_id": "brs_cube00000000c3" },
               { "kind": "brush", "brush_id": "brs_cube00000000d4" } ] }
```

Declaration order in `groups` is **not** nesting order: a child may be declared
before or after its parent, and a reader resolves by `group_id`.

**Expansion** — the objects a group stands for — is the union of its object
members and the expansion of each child group. It is **deterministic** (members
in member-list order, children expanded depth-first at the position they are
listed) and **de-duplicated**: an object reached twice is reached once, so a
transform applied to an expanded group moves each brush and entity exactly
once. A reader expands **iteratively** with a visited set, never by unbounded
recursion; a document that nests deeper than 32 or contains a cycle is refused
with SEM-G-9 or SEM-G-8, never with a stack overflow.

### Editing the graph

What an editor does to the graph, stated as before → after so that every
implementation makes the same document. `P` is a parent group, `C` a child
group, `x`/`y` objects.

| operation | before | after | ids |
| --- | --- | --- | --- |
| delete object `x` | `P{x, y}` | `P{y}` | `P` keeps its id |
| delete the last object | `C{x}` inside `P{C, y}` | `C` dissolved, `P{y}` | `P` keeps its id; `C`'s id is gone |
| delete a whole group `C` from the map (geometry too) | `P{C, y}`, `C{x}` | `P{y}`; `x` deleted | as above |
| remove member `x` from `C` | `P{C, y}`, `C{x, z}` | `P{C, y}`, `C{z}` — `x` becomes loose | all ids kept |
| **Ungroup** `C` | `P{C, y}`, `C{x, z}` | `P{x, z, y}` — `C`'s direct members move up **in place of `C`**, to `C`'s parent | `P`, `x`, `z` keep ids; `C`'s id is gone |
| Ungroup a root `P` | `P{C, y}` | `C` becomes a root, `y` loose | `C`, `y` keep ids |
| group a parent with loose objects | `P{C, y}`, loose `u`, `v` | `N{P, u, v}` | new `N` minted; `P`, `C` keep ids and structure |
| reparent `C` into `Q` | `P{C, y}`, `Q{w}` | `P{y}`, `Q{w, C}` | all ids kept; a `P` left empty dissolves |
| **copy / paste** a selected `P` | `P{C, y}`, `C{x}` | a second tree `P'{C', y'}`, `C'{x'}` | **every** group and object in the copy gets a fresh id; inner membership is preserved; `source` is preserved (§9a `source`) |

Ungroup never deletes geometry, and nothing flattens a hierarchy on Save. A copy
that reaches only part of a group copies no group at all for it (the same rule
1.3 had for `source`: a subset carries no provenance). A format that cannot
encode hierarchy — `.map` among them — says so where it is written (§14) and
never persists half a graph.

### Containment normalization

Direct membership must not hold both an object and its own descendant:

```text
entity + one of its owned brushes  ->  keep the entity
brush  + one of its faces          ->  keep the brush
```

Both spellings would name the same geometry twice, and the member count — which
a user reads — would stop meaning anything.

From 1.4 the rule reaches **across the nesting**: within one group tree (a root
and everything below it) an object and one of its own ancestor objects must not
both be direct members anywhere. The root's expansion would otherwise name the
same geometry twice. Two *unrelated* trees may still hold an entity in one and
one of its brushes in the other, exactly as two unrelated 1.3 groups could.

### `source` — where the group came from

Added by 1.3. **Optional**, and the only member of a group that is not about
this document's own objects:

```json
{
  "group_id": "grp_entrance00001",
  "name": "Entrance Columns",
  "members": [ ],
  "source": { "kind": "prefab", "prefab_id": "user/qk3m2p9x7v1a0zt/9f2c41ab7d0e6538" }
}
```

It is **historical origin, not a live binding**. `source.kind: prefab` with
`source.prefab_id: X` means exactly one thing:

```text
this group was created by placing prefab X
```

It does **not** mean the geometry still equals X, that later edits to X reach
this group, that edits here reach X, or that deleting X from a library deletes
or invalidates this group. Everything else follows from that reading:

| what happens | what happens to `source` |
| --- | --- |
| the group is renamed | preserved |
| a member is added or removed | preserved |
| the contained geometry is moved, rotated, retextured | preserved |
| the whole group is cloned, copied or imported | preserved, under a **fresh** `group_id` |
| a *subset* of members is copied | no group is created, so no `source` |
| the group is ungrouped | gone with the group |
| a group is created from a selection | absent |

`prefab_id` is an **external** identity — the prefab library's, not this
document's. It carries no APMap id prefix, nothing in this document declares it,
`SEM-G-1` does not apply to it, and it is **never reminted** when the document's
own ids are: that is what makes a cloned group still remember the same prefab.
The contract bounds it (1–128 characters) and imposes no pattern, because the
shape belongs to whichever library issues it.

**The identity is all that is stored.** No screenshot bytes, no image URL, no
host, no title, no owner, no revision, no extraction job id. Those are the
library's, and the library can revoke, replace or re-render them; a copy here
would be a stale, unauthenticated second source of truth for somebody else's
asset. A consumer resolves the picture from `prefab_id` at the moment it draws,
and when it cannot — the prefab was deleted, or belongs to an account this
reader cannot see — the provenance still stands and the picture is simply
absent. `additionalProperties: false` is what enforces that, and a producer that
wants to add one more field is proposing a schema change.

`kind` is a closed constant rather than an open string for the same reason a
`derived_from` kind is: a consumer meeting an unknown origin could only guess
what the group's history was.

## 9b. Authorship

Added by 1.4. **Optional**, and the one place a document says who made it:

```json
"authorship": {
  "author": "Zoë O'Neil & Bob \"the Builder\"",
  "copyright_notice": "© 2026 Zoë O'Neil.\nAll rights reserved.",
  "license": "CC-BY-SA-4.0"
}
```

| member | meaning | bound | line breaks |
| --- | --- | --- | --- |
| `author` | the author or authors, as one line the author wrote | 256 | no |
| `copyright_notice` | the copyright notice, verbatim | 1024 | LF allowed |
| `license` | the stated licence: a name, an SPDX expression, or its wording | 4096 | LF allowed |

Bounds count Unicode code points. TAB is allowed where line breaks are; CR and
every other control character (C0, DEL, C1) are refused by the schema, so line
breaks are always LF (SER-2) — an editor converts CRLF at the input, never in
the document.

### Three states, never two

| spelling | means |
| --- | --- |
| member absent | **unknown** — nothing was recorded |
| `""` | **deliberately blank** — somebody chose to record nothing |
| any other string | the text, exactly as supplied |

`authorship: {}` is refused (`minProperties: 1`): "nothing recorded" has one
spelling, omit the member, so two writers of the same document emit the same
bytes. `null` is not a state.

A destination that *requires* a value (a publishing form, a gallery) asks only
for the member that is **absent**, and it records the answer as supplied — an
empty answer is `""`, not absence. It never fills an absent member from the
signed-in account, the uploader, or a previous document.

### Verbatim text

Text is stored, compared and re-emitted **exactly**. No reader, writer, promotion
or transport trims it, case-folds it, re-punctuates it, re-escapes it beyond
SER-9, or applies Unicode normalization: an NFC and an NFD spelling of the same
name are two different values, and each survives as written. A string that is
not Unicode text — an unpaired surrogate escape such as `"\ud83d"` — is refused
(SEM-A-1), because the only thing a reader could do with it is replace it with
U+FFFD, which silently changes the author's words.

### What it is not

- **Not identity, not the account.** `author` is who the document says made it.
  It is never the uploader, the signed-in user, or the account that saved a
  revision; those are platform facts and live on the platform (AUB), not here.
  The schema closes the object so that an `uploader` member cannot be added.
- **Not permission.** `license` is words somebody typed. Nothing may read it to
  decide what a user is allowed to do, no component chooses a licence on the
  author's behalf, and a free-text licence never manufactures a right.
- **Not the whole of provenance.** Several contributors, SPDX-structured
  licences or provenance chains belong in a namespaced `extensions` entry
  (§9), which every consumer already preserves. The three members answer the
  question an editor has to answer today and leave room for more.

### Derived documents

When a prefab, extraction or excerpt is made from a document:

- the **source's** `authorship` is carried into the derived document unchanged
  when it remains true — the geometry is still that author's work;
- the person or pipeline that performed the derivation is **not** written into
  `author`; that is operation provenance (`derived_from` kind `operation`,
  `provenance.producer`), not authorship;
- nothing is inferred: a source with no `authorship` yields a derived document
  with none, and no licence is manufactured from the source's free text.

`.map` has no place for any of this (§14): exporting to `.map` loses
`authorship`, and only `.map` does.

## 9c. Quake III patches (1.5)

A **patch** is a Quake III bezier surface (`patchDef2`): a grid of control points
that a renderer or a compiler tessellates into triangles. It is an entity content
item beside properties and brushes:

```json
{
  "kind": "patch",
  "patch_id": "pat_q3arch00000001",
  "texture": "base_trim/arch",
  "width": 3,
  "height": 3,
  "control_points": [
    [[0, 0, 64, 0, 0], [0, 8, 80, 0, 0.5], [0, 0, 96, 0, 1]],
    [[16, 24, 64, 0.5, 0], [16, 32, 80, 0.5, 0.5], [16, 24, 96, 0.5, 1]],
    [[32, 0, 64, 1, 0], [32, 8, 80, 1, 0.5], [32, 0, 96, 1, 1]]
  ],
  "tail": [0, 0, 0]
}
```

| member | meaning |
| --- | --- |
| `patch_id` | stable identity (§3.3), never derived from geometry |
| `derived_from` | provenance, exactly as for a brush (§3.5, path `e<n>.p<n>`) |
| `texture` | the shader name, written as a face's `texture` is: no `textures/` prefix |
| `width`, `height` | the grid's dimensions: odd, 3 to 31 |
| `control_points` | `width` columns of `height` points, in patchDef2 text order |
| `tail` | the three numbers after width and height in the patchDef2 header, verbatim |

### The grid

`control_points[c][r]` is column `c` (0 ≤ c < width), row `r` (0 ≤ r < height),
and is the point Q3Map2 stores at `verts[r · width + c]` — the order the numbers
appear in the `.map` text, so an importer writes what it reads and an exporter
reads what it writes. Each point is `[x, y, z, s, t]`: a position in the
document frame (§4) and a texture coordinate in texture units.

| limit | value | why |
| --- | --- | --- |
| dimensions | odd, 3 … 31 | the grid is a mesh of 3×3 quadratic sub-patches sharing their edges; the engine refuses even sizes, and Q3Map2 refuses a dimension above 32 (`ParsePatch: bad size`, measured) |
| points | at most 31 × 31 = 961 | follows from the dimensions; enforced by `maxItems` before anything counts |
| every component | within ±131072 | Quake III's `MAX_WORLD_COORD`; also what makes a non-finite value a schema error |
| grid = width × height | SEM-Q-2 | the schema cannot compare an array's length with a sibling member |

A patch whose points are degenerate (collinear, coincident) is **not** a document
fault: Quake III accepts it and the compiler drops what it cannot use. Validity
here is structural, never a verdict on the geometry.

### What a patch is not

- **Not a solid.** It has no half-spaces, it does not seal a map, and no CSG
  operation (carve, hollow, merge, clip) applies to it. A tool that needs a
  closed solid leaves patches alone rather than treating one as a brush.
- **Not a mesh.** Tessellation — how many triangles a curve becomes — is decided
  by whoever draws or compiles the patch and is **never stored**. Two renderers,
  or a preview and Q3Map2, may subdivide the same patch differently; the control
  grid is the only source of truth.
- **Not `patchDef3`.** patchDef3 carries an explicit subdivision; it is a Doom 3 /
  Quake 4 grammar that Q3Map2 does not read (measured: `MatchToken( "(" )
  failed`). 1.5 has no subdivision member, and an importer refuses patchDef3
  rather than dropping its subdivision. A later minor version can add an optional
  member once a supported compiler gives it a meaning.

### Ownership, groups, editing, persistence

| concern | rule |
| --- | --- |
| owner | the entity whose `content` lists it — worldspawn or a brush entity — exactly like a brush. Deleting the entity deletes its patches. |
| position | its place in `content`, preserved; content order is semantic (SER-6) |
| groups | a group member `{ "kind": "patch", "patch_id": … }` (§9a). One direct parent group at most (SEM-G-4); an entity and its own patch are never both reached by one group tree (SEM-G-5); deleting the patch removes its membership in the same transaction |
| transform | moving, rotating or scaling a patch rewrites its control-point positions; `s`/`t` are unchanged unless an editor applies a texture operation. There is no per-patch transform member. |
| copy / paste | a fresh `patch_id`, the same grid, texture and tail; group membership follows §9a's copy rule |
| history | a patch edit is one map transaction and one undo step, like a brush edit |
| persistence | the whole item is saved, stored and relayed **verbatim** — every storage and transport keeps it byte-for-byte at the value level, and none may drop it because it does not understand it |

A component that cannot yet preserve patches **refuses** a document that has
one, by name, before changing anything. It never strips the item, flattens it
into a brush, or saves the rest of the document without it.

## 10. Rules enforced by JSON Schema

| id | rule |
| --- | --- |
| SCH-1 | `apmap_version` is `const "1.0"` |
| SCH-2 | `apmap_version`, `document_id`, `revision`, `role`, `game`, `map_dialect`, `id_policy`, `frame`, `entities` are required |
| SCH-3 | `revision` is an integer ≥ 0 |
| SCH-4 | `role` is one of the four enumerated roles |
| SCH-5 | every core object sets `additionalProperties: false`; undeclared members are rejected |
| SCH-6 | a brush has at least 4 faces |
| SCH-7 | a face plane has exactly 3 points, each a 3-number array |
| SCH-8 | a `valve220` projection requires `u_axis` and `v_axis` |
| SCH-9 | a `classic` projection is closed; Valve 220 members cannot be mixed in |
| SCH-10 | object ids match their typed prefix pattern |
| SCH-11 | an `extensions` namespace contains at least one dot |
| SCH-12 | entity `content` has at least 1 item |
| SCH-13 | `to_source` is `identity` or `translation`; `affine` is reserved and rejected |
| SCH-14 | a face requires `face_id`, `plane`, `texture`, `projection` |

Added by 1.2, alongside `groups` (§9a):

| id | rule |
| --- | --- |
| SCH-G-1 | `groups` is required and is an array; a current writer always emits it, empty or not |
| SCH-G-2 | a group requires `group_id`, `name`, `members`; it is closed |
| SCH-G-3 | `group_id` matches `^grp_[0-9A-Za-z]{8,64}$` |
| SCH-G-4 | `name` is 1–128 characters and contains at least one non-whitespace character |
| SCH-G-5 | `members` has at least 2 items and no duplicates |
| SCH-G-6 | a member is one of `entity`/`brush`/`face`, each closed and carrying only its own kind's id field |

Added by 1.3, for `group.source` (§9a):

| id | rule |
| --- | --- |
| SCH-G-7 | `source` is optional; a group that omits it has no recorded origin, which is not an error |
| SCH-G-8 | `source` requires `kind` and `prefab_id`, and is closed — no screenshot, URL, title, owner or job id |
| SCH-G-9 | `kind` is `const "prefab"`; `prefab_id` is a string of 1–128 characters, with no imposed pattern |

Added by 1.4, for nesting and `authorship` (§9a, §9b):

| id | rule |
| --- | --- |
| SCH-G-5′ | from 1.4, `members` has at least **1** item (was 2) and still no duplicates; 1.2–1.3 documents keep SCH-G-5 |
| SCH-G-10 | a member may be `{ "kind": "group", "group_id": … }`, closed, `group_id` matching SCH-G-3 |
| SCH-A-1 | `authorship` is optional, closed, and has at least one member; it holds only `author`, `copyright_notice`, `license` |
| SCH-A-2 | each member is a string; `author` ≤ 256, `copyright_notice` ≤ 1024, `license` ≤ 4096 code points |
| SCH-A-3 | `author` holds no control character (C0, DEL, C1); `copyright_notice` and `license` hold none except LF and TAB |

Added by 1.5, for the Quake III surfaces (§6.2a, §9c):

| id | rule |
| --- | --- |
| SCH-Q-1 | an entity content item may be `patch`; a patch requires `kind`, `patch_id`, `texture`, `width`, `height`, `control_points` and is closed |
| SCH-Q-2 | `patch_id` matches `^pat_[0-9A-Za-z]{8,64}$` |
| SCH-Q-3 | `width` and `height` are each one of 3, 5, … 31 |
| SCH-Q-4 | `control_points` holds 3–31 columns of 3–31 points; a point is exactly 5 numbers, each within ±131072 |
| SCH-Q-5 | a patch `texture` is non-empty and holds no whitespace or double quote (the face `texture` rule) |
| SCH-Q-6 | a patch `tail`, when present, is exactly 3 numbers |
| SCH-Q-7 | a face projection may be `brush_primitives`: `mode` and `matrix` only, the matrix exactly 2 rows of 3 numbers, each within ±1 000 000 |
| SCH-Q-8 | a group member may be `{ "kind": "patch", "patch_id": … }`, closed |
| SCH-Q-9 | `source_path` / `object_path` may address a patch, `e<n>.p<n>` |

## 11. Semantic validation rules

These cannot be expressed in JSON Schema. A conforming validator enforces them
after schema validation.

| id | rule |
| --- | --- |
| SEM-1 | every `entity_id`, `brush_id` and `face_id` is unique across the whole document |
| SEM-2 | every `relationship.from` / `.to` references an id declared in this document |
| SEM-3 | `synthetic` and `synthetic_reason` appear together; `synthetic` must be `true` |
| SEM-4 | `space: source_world` requires `to_source.type: identity` |
| SEM-5 | a face's three plane points are not collinear |
| SEM-6 | a brush's half-space intersection encloses a finite, non-degenerate volume |
| SEM-7 | every declared face contributes a bounded facet; a face that contributes none is redundant (**warning**, not an error) |
| SEM-8 | under `id_policy: derived`, every object carries `derived_from` |
| SEM-9 | a `full_map` document that declares any entity contains exactly one `worldspawn`, and it is first. An empty `entities` array is legal (**recommended** for other roles) |
| SEM-10 | under `id_policy: derived`, an object's id equals the digest of its `derived_from` identity inputs (§3.5) |
| SEM-11 | every number is finite; `NaN`, `Infinity` and `-Infinity` are forbidden |

SEM-11 needs its own check because JSON Schema's `number` type accepts anything
a JSON parser produced, and `1e400` decodes to `Infinity` in most parsers.

Groups add more, none of them expressible in JSON Schema because every one
is a statement about the rest of the document:

| id | rule |
| --- | --- |
| SEM-G-1 | every member reference resolves to an object — or, from 1.4, a group — declared in this document |
| SEM-G-2 | a member's `kind` matches the referenced object's actual kind |
| SEM-G-3 | every `group_id` is unique across the document |
| SEM-G-4 | no object is a direct member of two groups; from 1.4, no **group** is a direct member of two groups either — one parent at most |
| SEM-G-5 | direct membership holds no ancestor/descendant object pair (§9a, containment normalization); from 1.4 this holds across a whole group **tree**, not only one member list |
| SEM-G-6 | a persisted group has at least two members (1.2–1.3) / **one** member (1.4); an editor dissolves a group that falls below, in the same transaction that took the member away, and a parent that the dissolution empties dissolves with it |
| SEM-G-7 | `source.prefab_id` is an EXTERNAL identity: SEM-G-1 does not apply to it, it is never reminted with the document's own ids, and an unresolvable one is not a document fault |
| SEM-G-8 | 1.4: the group graph is acyclic — no group reaches itself, directly (a self-link) or through any chain of children |
| SEM-G-9 | 1.4: groups nest at most **32** deep, counted in groups from a root to its deepest descendant; a reader traverses iteratively and reports this rule, never a stack overflow |

`authorship` adds one (§9b):

| id | rule |
| --- | --- |
| SEM-A-1 | every `authorship` string is Unicode text: no unpaired surrogate. A reader whose JSON decoder would replace one with U+FFFD must refuse the document rather than re-emit it |

The Quake III surfaces add five (§6.2a, §9c):

| id | rule |
| --- | --- |
| SEM-Q-1 | every `patch_id` is unique across the document (SEM-1, extended to the fourth object kind) |
| SEM-Q-2 | a patch's grid is exactly `width` columns of exactly `height` points |
| SEM-Q-3 | a `brush_primitives` matrix is non-degenerate: `m00·m11 − m01·m10 ≠ 0`, computed in IEEE-754 double precision on the numbers as written |
| SEM-Q-4 | a `patch` or a `brush_primitives` face appears only in a document whose `game` is `quake3` |
| SEM-Q-5 | a brush's faces are all `brush_primitives` or none of them is |

SEM-8 and SEM-10 cover a patch like any other object (§3.5, kind `patch`);
SEM-11 is enforced for patch points and matrix coefficients by their schema
bounds. SEM-G-1/2/4/5 treat a patch as an object its entity owns.

`test-vectors/index.json`'s `semantic_invalid` set holds one schema-valid
document per rule above that 1.4 or 1.5 added or changed, each naming the rule a
conforming semantic validator must report. `test/helpers.mjs`'s
`semanticFaults` is the reference implementation those vectors are proved
against.

A current-format document with a dangling group member is **invalid**. When an
edit deletes a member object, group membership is updated in the *same* map
transaction — there is no window in which the document is inconsistent, and no
dangling one-member group is ever written.

## 12. Canonical serialization rules

Two producers given the same document must emit the same bytes.

| id | rule |
| --- | --- |
| SER-1 | UTF-8, no byte-order mark |
| SER-2 | LF line endings only |
| SER-3 | two-space indentation |
| SER-4 | exactly one trailing newline at end of file |
| SER-5 | object members in the order this specification declares them — `authorship` follows `provenance`, `groups` sits between `entities` and `relationships`, a group is `group_id`, `name`, `members`, `source`, a group member is `kind` then its id field, `authorship` is `author`, `copyright_notice`, `license`, a patch is `kind`, `patch_id`, `derived_from`, `texture`, `width`, `height`, `control_points`, `tail`, `extensions`, and a brush-primitives projection is `mode`, `matrix` |
| SER-6 | arrays in **semantic** order — entity, content, face and relationship order is meaningful and is never sorted |
| SER-7 | numbers are finite; a value that is mathematically an integer is emitted as a JSON integer; other values are rounded to 6 decimal places; `-0` is emitted as `0`; no exponent notation |
| SER-7Q | 1.5: the five numbers of a patch control point and the six coefficients of a `brush_primitives` matrix are **not** rounded: each is emitted as the shortest decimal that parses back to the same IEEE-754 double, in positional notation (no exponent); integers as integers, `-0` as `0`. Every other number, a patch `tail` and a face `tail` included, still follows SER-7 |
| SER-8 | a document that must be reproducible carries no wall-clock timestamp. `provenance.generated_at` is permitted but forfeits byte determinism |
| SER-9 | non-ASCII characters are emitted literally, not `\u`-escaped |

SER-7 matches AUE's MAP writer, which emits `192` rather than `192.0`,
so nothing is lost crossing between the two formats.

SER-7Q exists because six decimal places is not enough for a Quake III surface:
a texture coordinate of `0.00260417` or a matrix coefficient of `0.0078125`
(1/128) would be changed by SER-7, and at x' = 4096 a coefficient rounded that
way shifts the texture by more than a quarter of a texel on a 128-pixel image.
Shortest round-trip keeps exactly the double the source's text parsed to, so
`.map` → APMap → `.map` re-emits the source's value, and it is still a fixed
point. A writer that implements SER-7 but not SER-7Q must not write a document
holding a patch or a brush-primitives face; it refuses instead (§9c).

Canonical encoding is a **fixed point**: decoding a canonical document and
re-encoding it must reproduce the original bytes. Every example and vector in
this directory is verified against that property.

## 13. Compatibility

Published schemas are **immutable**. Changing the meaning of an existing schema
is forbidden; a format change produces a new schema file and an explicit
migration.

```text
apmap/1.0/apmap.schema.json
apmap/1.1/apmap.schema.json
apmap/1.2/apmap.schema.json
apmap/1.3/apmap.schema.json
apmap/1.4/apmap.schema.json
apmap/1.5/apmap.schema.json
apmap/2.0/apmap.schema.json
```

- **Minor version (1.x)** adds members and reserved-name semantics only. It
  never removes a member, narrows a type, or changes a meaning. A minor version
  may make a *new* member required of its own writers — 1.2 does, with `groups`
  — because that constrains only producers that declare the new version, and a
  reader of an older document is unaffected.
- **Major version (x.0)** may break anything.
- Because every core object is closed (SCH-5), a 1.0 validator **rejects** a 1.1
  document rather than silently ignoring its new members. Consumers must
  validate against the version a document declares and must not accept a
  version they do not implement.
- Forward-compatible data belongs in `extensions`, which is the only member a
  1.0 consumer must preserve without understanding.
- **Promotion** of a legacy document to the current contract rewrites the
  declared version and supplies the structural defaults the current contract
  requires — today exactly one, `groups: []`. It rebuilds no geometry, remints
  no id and rewrites no provenance, which is what makes it mechanical. It also
  **invents nothing**: 1.3's `group.source` is optional, so a promoted 1.2 group
  keeps its id, its name and its members and records no origin, because none was
  ever recorded. A minor version that needed a *value* rather than a structural
  default would not be promotable this way, and that is the constraint on adding
  one.
- **1.4 is the first minor version with a relaxation.** It lowers a persisted
  group's minimum from two members to one. That changes no meaning a 1.3
  document can express — every valid 1.3 group is a valid 1.4 group unchanged —
  it only admits groups 1.3 refused. It is written down here and in
  `test/schema.test.mjs`, which allows exactly this one difference and fails on
  any other, so the next relaxation has to be argued in the same two places.
  Readers that assumed "at least two" (a card, a dissolve-at-one reducer, a
  result-group builder) are consumer migrations, listed in the 1.4 handoff.
  Promotion from 1.3 remains the header alone: `authorship` stays absent
  (unknown), and no group is nested that was not.
- **1.5 has the second relaxation.** The object-path grammar — `source_path`
  and `object_path` — admits `e<n>.p<n>` so a patch can be addressed. Every path
  1.4 accepted is accepted and means what it meant; the only new spelling is a
  patch's. `test/schema.test.mjs` allows exactly these two pattern changes
  between the frozen 1.4 schema and 1.5. Everything else 1.5 adds is a new arm
  of an existing union (content item, projection, group member) or a new
  definition, so promotion from 1.4 is the header alone and a promoted document
  gains no patch, no matrix and no path it did not have.

## 14. `.map` import and export loss boundaries

```text
.map import → new APMap document + stable ids
APMap edit  → canonical collaborative state
APMap export → .map representation
```

### Preserved exactly

entity order · repeated keys · key order relative to brushes · brush order ·
face order · 3-point planes · classic and Valve 220 projections · Quake 2
numeric face tails · texture names · brushless point entities · unknown entity
keys, verbatim · from 1.5: patches (grid, texture coordinates, shader, header
tail) at their content position, and brushDef texture matrices, for an importer
that implements them

### Lost on import (`.map` → APMap)

| lost | why |
| --- | --- |
| `//` comments | the MAP tokenizer discards them at the line level |
| whitespace and indentation | the writer re-emits its own layout |
| number spelling (`0.50` vs `0.5`, `+3` vs `3`) | numbers are re-normalized by SER-7 |
| source line and column | only entity/brush/face indices survive, in `derived_from` |
| brush primitives, bezier patches, Quake 3 patch meshes (**until 1.5**) | 1.0–1.4 have no place for them; an importer that does not implement 1.5's §6.2a/§9c still refuses a source containing them rather than dropping them |
| `patchDef3` and `brushDef3` | Doom 3 / Quake 4 grammars Q3Map2 does not read (measured); 1.5 does not represent them and a Quake III importer refuses them |

An unedited `.map` therefore re-exports **semantically identical, not
byte-identical**. That is the same guarantee AUE's extraction round-trip
equivalence check proves, at 0.0 plane error and 0.0 UV error.

### Lost on export (APMap → `.map`)

`.map` has nowhere to record APMap identity. Every `document_id`, `entity_id`,
`brush_id`, `face_id`, `derived_from`, `relationship` and `extensions` value is
dropped, and so are `groups` — the whole hierarchy, parents and children — and
`authorship`. A Quake III `.map` keeps patches and brushDef matrices (they are
`.map` syntax), but not their ids; one `.map` file uses one brush syntax, so an
exporter facing a document that mixes brushDef and classic brushes converts
(given the image sizes) or refuses. Each brush and entity is written **once**, whatever groups reach
it, so grouping never changes the compiled world. The loss is `.map`'s alone:
an `.apmap` export carries all of it, and reimporting that `.apmap` restores
the exact hierarchy and authorship. **Reimporting an exported `.map` without its APMap sibling creates a
new document with new ids.** This is why `.map` is an import/export
representation and APMap is canonical.

The frame is also flattened: exporting a `prefab_local` document writes local
coordinates, and the `to_source` translation survives only if a sidecar records
it.

### Source data APMap 1.0 deliberately does not represent

Recorded here so nobody looks for it:

- **Package sidecar content** — classification, behaviour, validation results,
  recipes, preview cameras and review evidence stay in `prefab.json`,
  `classification.json`, `provenance.json`, `validation.json` and `recipe.json`.
  APMap is the geometry layer, not the package.
- **Derived render state** — AUP's `dropped_faces`, `geometry_status`,
  `alternate_renderable_faces`, face polygons, bounds and centres are recomputed
  from geometry and are never stored.
- **Texture image data** — only texture names. WAD resolution stays with the
  consumer.
- **Compiled artifacts** — `.bsp`, `.prt`, lightmaps, visibility.
- **Quake 2 surface flag semantics** — the numbers survive in `tail`; their
  meaning is not defined in 1.0.

---

## 15. Examples and test vectors

`examples/prefab-core.apmap` is a real conversion of the extracted package
`multi-brush-rigid-e1m4-e30-e31`: the opposed `func_door` pair at source
entities 30 and 31 of `e1m4.map`, in prefab-local coordinates with the
`canonicalize_rigid` translation `[1224, 999, 1384]` recorded in `frame`, plus
the `assembly_member` relationship for the pair. Its object ids are real
digests over that source map's SHA-256.

Every document in `examples/` and `test-vectors/valid/` passes schema validation
and every semantic rule. Every document in `test-vectors/invalid/` is rejected,
and `test-vectors/invalid/README.md` and `index.json` name the exact rule each
one violates.

Vector coverage spans: minimal document · real prefab core · Valve 220 ·
Quake 2 face tails · repeated entity keys · cropped-context synthetic faces ·
journal-derived authored ids · typed relationships · brushless point entities ·
namespaced extensions.
