# APMap 1.4 — deprecated, and readable

**Deprecated means "not current / never written", not "unreadable".** 1.4 is a supported legacy
READ format: a service opens a 1.4 document, validates it against the frozen 1.4 contract, and
promotes it to current before anything is written or sent to collaboration.

```text
schema/deprecated/apmap-1.4.schema.json   the CONTRACT — a runtime input, shipped with the package,
                                          loaded at startup by every backward-reading service
deprecated/1.4/  (this directory)         the CORPUS — historical evidence, not shipped, not a
                                          runtime input, referenced only by this repository's tests
```

The contract file was **moved, not rewritten**, when 1.5 was promoted. Git records it as a pure
rename with zero content changes, and `test/schema.test.mjs` asserts that its `apmap_version` enum
is still exactly `["1.0", "1.1", "1.2", "1.3", "1.4"]`, that its entity content is still
`property`/`brush` only, that a face projection is still `classic`/`valve220` only, that a group
member is still one of `entity`/`brush`/`face`/`group`, and that its object-path grammar still
refuses `e<n>.p<n>`. Those facts are what 1.4 said, and a legacy 1.4 document is still held to them.

## What was current when 1.4 was current

```text
one-four-document.apmap       a valid, otherwise-ordinary 1.4 document. The fixture a consumer
                              uses to prove its version gate reads a deprecated document as that
                              version — it differs from a current one by its header alone.
test-vectors/valid/           the sixteen documents 1.4 required to be accepted
test-vectors/invalid/         the twenty-five it required to be rejected, each naming its rule
test-vectors/semantic-invalid/ the eleven schema-valid documents a 1.4 semantic validator refused,
                              each naming its SEM-* rule
```

Their 1.5 descendants are in the package root's `test-vectors/`: the same documents promoted by
their header alone, plus what 1.5 added. `no-1-4-features.apmap` moved here whole; its 1.5
counterpart, `no-1-5-features.apmap`, is the same body with a 1.5 header.

## Why 1.4 stopped being current

A Quake III map could not be written in 1.4. Two of its source constructs had no place in the
document, and a converter had only bad choices — refuse the map, or drop what it could not say:

1. **Bezier patches** (`patchDef2`). 1.4's entity content was properties and brushes; a curved
   surface is neither, and is not a solid at all.
2. **brushDef faces** ("brush primitives"). A 2x3 texture matrix has no lossless `classic` or
   `valve220` spelling: converting it needs the image size, which a document does not hold, and a
   matrix with shear has no classic form whatever the size.

1.5 adds the content item `patch`, the projection mode `brush_primitives` and the group member kind
`patch`, and widens the object-path grammar so a patch can be addressed (`e<n>.p<n>`).

## Promotion from here is still the header alone

Every 1.5 addition is a new arm of an existing union and nothing new is required, so a 1.4 document
is a valid 1.5 document once its version is rewritten: same ids, same groups, same `authorship`,
same bytes below the header. `promoteToCurrent` in `test/helpers.mjs` is the one place these tests
express the promotion, and `test/promotion.test.mjs` proves it over this corpus.
