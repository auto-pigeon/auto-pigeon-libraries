# Where go/mapio came from

This code was written for `auto-pigeon-extractor` and moved here by its copyright owner, who decided
to publish the selected implementation under this repository's Apache-2.0 licence (`NEW_371`,
2026-10-30). That decision covers what is listed here and nothing else: it does not relicense the rest
of `auto-pigeon-extractor`, which stays proprietary, or change the terms of anything already
distributed.

**Source:** `auto-pigeon-extractor` at commit `dce4c6a8e3934ac3fbe4664430eadd48c8e08073`.

| original path | here | notes |
| --- | --- | --- |
| `internal/convert/convert.go` | `go/mapio/convert.go` | `Convert` gained a context, typed warnings and refusals, and manifest bytes |
| — | `go/mapio/validate.go`, `capabilities.go` | new: the on-demand validation and capability API |
| `internal/apmap/*.go` except `publish.go`, `resolve.go`, `wounds/`, `cmd/` | `go/mapio/apmap/` | path, directory and environment helpers removed; explicit contract and context options added |
| `internal/mapfile/*` | `go/mapio/mapfile/` | `ParseFile` and `WriteFile` removed |
| `internal/rmf/*.go` | `go/mapio/rmf/` | `DecodeFile` and `EncodeFile` removed; `EncodeContext` added |
| `internal/geometry/convex/*` | `go/mapio/convex/` | unchanged but for comments and one import |
| `internal/pysum/pysum.go`, `pysum_test.go` | `go/mapio/pysum/` | unchanged but for comments |
| `internal/convert/testdata/`, `*_test.go` | `go/mapio/testdata/`, `go/mapio/*_test.go` | synthetic fixtures, authored for these tests |
| `internal/apmap/embedded/schema` | `go/mapio/apmap/embedded/schema` | regenerated from `ts/apmap-schema/schema` by `internal/schemasync`; the bytes of every schema are identical |

## The Quake III importer (`NEW_389`)

`quake3map/`, `legality/`, `apmap/quake3import.go` and `quake3import.go` are a Go port of this
repository's own `ts/mapio` (Apache-2.0, moved from the Auto-Pigeon editor by `NEW_388` under its
owner's publication decision): `src/map/quake3.ts` (reader), `src/apmap/quake3Encode.ts` (encoder),
and `src/geometry/brushLegality.ts`, `convexity.ts`, `reconstructBrush.ts`, `quakeWinding.ts`,
`brushPrimitives.ts` (admission). No other source was used. The tokenizer reproduces Q3Map2's
`scriplib.c` rule as a behaviour, measured, not copied; `Hypot` reproduces V8's documented
`Math.hypot` algorithm. The conformance fixtures under `ts/mapio/test/fixtures/` are authored.

Tests that need a private corpus stayed with their owner: the Worldcraft `.rmf` export comparison, the
Quake II oracle pairs, and the real RMF 1.6 document.

## Third-party material

No source file here carried a third-party copyright or licence header at the source commit. Three
kinds of outside knowledge are present, recorded so a reader does not have to infer them:

- **`pysum`** reproduces the behaviour of CPython 3.12's builtin `sum()` over floats, a Neumaier
  compensated summation. CPython is distributed under the PSF License; see `NOTICE`.
- **`apmap/classicfidelity.go`** computes the Classic texture projection the way Quake-family compilers
  read a `.map` (base plane by normal, rotate, scale, shift). That is the format's meaning, written
  independently here and in the editor, and it was measured against compiler output rather than copied
  from compiler source.
- **`rmf`** follows the on-disk layout of Worldcraft's `.rmf`, measured from real files and public
  community notes. No Worldcraft code is involved.

Test fixtures are synthetic. No game asset, private map or commercial dataset is included.
