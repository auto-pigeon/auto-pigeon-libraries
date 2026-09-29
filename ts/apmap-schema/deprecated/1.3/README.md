# APMap 1.3 — deprecated, and readable

**Deprecated means "not current / never written", not "unreadable".** 1.3 is a supported legacy
READ format: a service opens a 1.3 document, validates it against the frozen 1.3 contract, and
promotes it to current before anything is written or sent to collaboration.

```text
schema/deprecated/apmap-1.3.schema.json   the CONTRACT — a runtime input, shipped with the package,
                                          loaded at startup by every backward-reading service
deprecated/1.3/  (this directory)         the CORPUS — historical evidence, not shipped, not a
                                          runtime input, referenced only by this repository's tests
```

The contract file was **moved, not rewritten**, when 1.4 was promoted. Git records it as a pure
rename with zero content changes, and `test/schema.test.mjs` asserts that its `apmap_version` enum
is still exactly `["1.0", "1.1", "1.2", "1.3"]`, that a group still needs two members, that a group
member is still one of `entity`/`brush`/`face`, and that it has no `authorship`. Those three facts
are what 1.3 said, and a legacy 1.3 document is still held to them.

## What was current when 1.3 was current

```text
one-three-document.apmap      a valid, otherwise-ordinary 1.3 document. The fixture a consumer
                              uses to prove its version gate reads a deprecated document as that
                              version — it differs from a current one by its header alone.
test-vectors/valid/           the ten documents 1.3 required to be accepted
test-vectors/invalid/         the sixteen it required to be rejected, each naming its rule —
                              including `group-one-member.apmap`, which 1.3 refuses and 1.4 accepts
```

Their 1.4 descendants are in the package root's `test-vectors/`: the same documents promoted, plus
what 1.4 added.

## Why 1.3 stopped being current

Three things the editor needed could not be written in 1.3:

1. **Who made the map.** 1.3 had no document-level place for an author, a copyright notice or a
   licence, so an exported `.apmap` lost them and the editor asked for them again on import. 1.4's
   optional `authorship` carries them.
2. **A group of groups.** 1.3's member union had no `group` arm, so "this group plus those two
   loose brushes" could only be written by flattening the inner group away.
3. **A group of one.** 1.3 required two members. A CUT that leaves a single cut-out brush on one
   side had to either lose the group or invent a sliver of geometry to satisfy a counter.

## Promotion from here is still the header alone

`authorship` is optional, and absent means *unknown* — so a promoted 1.3 document gains no author
and no licence it never declared. A 1.3 group has at least two members of the three object kinds,
which is a valid 1.4 group unchanged: same `group_id`, same name, same members, same `source`.
`promoteToCurrent` in `test/helpers.mjs` is the one place these tests express the promotion, and
`test/promotion.test.mjs` proves it over this corpus.
