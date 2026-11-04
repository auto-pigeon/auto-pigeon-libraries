# go/mapio

Reads, writes, converts and validates Quake map formats, in Go, as bytes in and bytes out. It ships
in the module `github.com/auto-pigeon/auto-pigeon-libraries`, under the repository's Apache-2.0
licence, and is what `auto-pigeon-extractor` and the Companion's `auto-pigeon-mapio` converter both
compile.

It is a library. It opens no file, reads no environment variable, starts no goroutine or timer, and
logs nothing; `TestTheLibraryTouchesNothingOutsideItsArguments` enforces that over the source of
every shipped file. There is no server, no Dockerfile and no process of its own here: a program that
wants a converter wraps this package.

## Conversion matrix

| direction | from | to | game written or read |
| --- | --- | --- | --- |
| `q1map-to-apmap` | Quake 1 `.map` | APMap | `quake1` |
| `apmap-to-q1map` | APMap | Quake 1 `.map` | `quake1` |
| `q2map-to-apmap` | Quake 2 `.map` | APMap | `quake2` |
| `apmap-to-q2map` | APMap | Quake 2 `.map` | `quake2` |
| `q2rmf-to-apmap` | Worldcraft `.rmf` (1.6, 1.8) | APMap | `quake2` |
| `apmap-to-q2rmf` | APMap | Worldcraft `.rmf` (1.8) | `quake2` |
| `q3map-to-apmap` | Quake III `.map` (classic, Valve 220, `brushDef`, `patchDef2`) | APMap | `quake3` |
| `apmap-to-q3map` | APMap (`quake3`) | Quake III `.map` plus a manifest | `quake3` |

RMF converts only through APMap. **The game is always the direction's.** A `.map` extension says
nothing about the game, and a classic Quake III face and a Quake 2 one look alike, so nothing here
detects one: the same bytes import as `quake1`, `quake2` or `quake3` according to the direction named.
A Quake 1 or Quake 2 read of a file holding `brushDef` or `patchDef2` is refused as `unsupported_game`
and names `q3map-to-apmap`.

Not here, and not claimed: Half-Life (the registry name is accepted by the schema, no direction reads
or writes it), Doom 3 / Quake 4 grammars (`brushDef3`, `patchDef3`, refused by name), BSP of any game.

## Quake III import

`q3map-to-apmap` is the Go port of the editor's importer, `ts/mapio` (`map/quake3.ts` and
`apmap/quake3Encode.ts`), and the two are held to each other: for the same file bytes and name they
write **the same canonical APMap bytes except `provenance.producer` and
`provenance.producer_version`**, which name the implementation (`apmap.Quake3ImportProducer`). The
shared conformance set is `ts/mapio/test/fixtures/q3-*.map` with `quake3-conformance/` beside it:
hand-derived facts both test suites check, and the TypeScript outputs this package must reproduce
(`TestQuake3Conformance*`).

```bash
# from a consumer: the Companion's converter, or the extractor's CLI
auto-pigeon-mapio convert --q3map-to-apmap --input room.map --output out --json
auto-pigeon-extractor convert --q3map-to-apmap --input room.map --output out
```

What it preserves and what it refuses, as the reference does:

- entity content in file order (a key after a brush stays after it, repeated keys stay repeated),
  classic and Valve 220 numbers through SER-7, every `brushDef` matrix coefficient and every patch
  control point verbatim (SER-7Q), face and patch tails, shader names minus one leading
  `textures/` (counted: `q3map_shader_prefix_removed`);
- identity: `source_map` derivation over the input's bare name and the SHA-256 of its **bytes**, the
  document id `<stem>:full_map`; the text is read as the editor reads a picked file (WHATWG UTF-8, BOM
  dropped);
- a plane that bounds nothing is left out when the brush without it is sound, and every such brush is
  listed (`q3map_redundant_planes`, `Result.Quake3Import.RedundantPlaneBrushes`); faces keep the ids
  their file positions give them; legality risks that do not refuse are counted (`q3map_import_risk`);
- the whole map is refused — `quake3_import`, with `Refusal.Detail` one of `quake3_import_parse` (every
  fault, with line and column: Doom 3 grammars, brush key/value pairs, mixed syntax in a brush,
  patch sizes outside 3..31 odd, malformed text), `quake3_import_brush_illegal` (each impossible brush
  by entity, brush and line; a zero-determinant `brushDef` matrix), `quake3_import_empty`, or an
  `apmap_encode_*` code — and nothing partial is ever returned.

Bounds: `MaxQuake3MapImportBytes` (64 MiB) and `apmap.MaxQuake3BrushFaces` (1024 faces per brush) are
refused as `input_too_large`; these two are this port's own and the reference has none. A
cancellation between entities, brushes or reconstructed faces is `cancelled`. Admission is decided by
`go/mapio/legality`, a port of the editor's brush legality whose arithmetic follows the reference
operation for operation (V8's `Math.hypot`, no fused multiply-add on any architecture), because a
tolerance comparison one ulp away would admit a different map.

## Convert

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio"
)

func main() {
	input, err := os.ReadFile("room.apmap")
	if err != nil {
		panic(err)
	}
	direction, _ := mapio.LookupDirection("apmap-to-q3map")
	output, result, err := mapio.Convert(context.Background(), direction, "room.apmap", input,
		mapio.Options{Validate: true})
	if err != nil {
		refusal := mapio.RefusalOf(err) // Code is stable; Message is the whole sentence.
		fmt.Fprintln(os.Stderr, refusal.Code, refusal.Message)
		os.Exit(1)
	}
	for _, warning := range result.Warnings {
		fmt.Println(warning.Code, warning.Text)
	}
	if err = os.WriteFile(result.OutputName, output, 0o644); err != nil {
		panic(err)
	}
	if result.ManifestName != "" {
		if err = os.WriteFile(result.ManifestName, result.ManifestBytes, 0o644); err != nil {
			panic(err)
		}
	}
}
```

Reading the input and writing the output are the caller's job. `Options.Contract` is `nil` for the
contract compiled into this module, which is the right answer for a standalone program; a caller told
to use another bundle parses it with `apmap.ParseContract` and passes it here. `Options.Schema` names
one schema and replaces version-matched validation.

`ctx` stops a conversion between its stages and between entities. A stopped conversion returns no
output, and `RefusalOf` reports `cancelled`.

Results carry typed warnings (`Warning{Code, Text}`), and errors classify through `RefusalOf` into
`validation_failed`, `unsupported_version`, `unsupported_game`, `quake2_admission`, `quake3_map`,
`quake3_surface`, `quake3_import`, `cancelled`, `input_too_large` or `invalid_input`. The original typed error stays
available to `errors.As`: `*ValidationError` keeps schema, semantic and identity problems apart.

## Validation runs when you ask

Decoding and validating are different calls:

```go
document, err := apmap.DecodeJSON(data)           // parses; passes no verdict
report, err := mapio.Validate(ctx, name, data, mapio.ValidateOptions{}) // the verdict, as a value
```

`Report` holds both layers (schema and `SEM-*`) and the identity subset, and says `Unreadable` for a
document that is not JSON or declares a version the contract does not hold. A failed document is a
report, not an error. No constructor, import, load, timer or background task validates anything, and a
conversion validates only with `Options.Validate`. Without it the **version gate** still runs: an
unreadable version is refused either way, because it is not a document validation could have
accepted.

## The packages

| import path | what it is | why it is public |
| --- | --- | --- |
| `.../go/mapio` | `Convert`, `Validate`, `CapabilitiesOf`, the direction table, typed results | the API |
| `.../go/mapio/apmap` | APMap documents: JSON values, canonical serialization, identity, the schema interpreter, `SEM-*`, `.map` conversion, the Quake III writer | a consumer builds documents from its own geometry |
| `.../go/mapio/mapfile` | the `.map` AST, parser, writer | a consumer analyses parsed maps |
| `.../go/mapio/rmf` | the Worldcraft `.rmf` reader and writer | a consumer reads `.rmf` entity tables |
| `.../go/mapio/convex` | exact convex brush reconstruction: planes, windings, polyhedra | a consumer shares the same solids |
| `.../go/mapio/quake3map` | the Quake III `.map` reader: WHATWG text decoding, Q3Map2's tokenizer, the ordered tree and its faults | a consumer reads Quake III maps without converting them |
| `.../go/mapio/legality` | the editor's brush legality: convexity, winding, duplicate planes, texture bases | a consumer admits brushes the way the importer does |
| `.../go/mapio/pysum` | compensated summation matching CPython's `sum()` | persisted numbers depend on it |

`go/mapio/internal/` holds what no consumer may reach: the contract test helper and `schemasync`.

Package-level `apmap` functions such as `Validate`, `SemanticProblems` and `ReadableVersions` read a
process default (`apmap.SetContract`) that nothing here sets. They exist for a service that loads one
contract at startup. `mapio.Convert` and `mapio.Validate` never read it.

## One schema authority

`ts/apmap-schema/schema` is the contract and the only copy anybody edits. Go cannot embed a parent
directory, so `apmap/embedded/schema` is a **generated snapshot** of it, carrying
`apmap-contract-provenance.json` (source path, package version, each file's sha256).

```bash
go run ./go/mapio/internal/schemasync          # rewrite the snapshot, from the repository root
go run ./go/mapio/internal/schemasync --check  # fail if it has drifted, changing nothing
```

`TestEmbeddedSnapshotMatchesTheCanonicalSchema` fails on any difference, in either direction, so a
schema change that forgets the snapshot fails `./run.sh test`. The snapshot names no repository
commit, since a file cannot hold the hash of the commit containing it. A binary's identity is the
module version in its build info: `mapio.BuildModuleVersion()`.

## Consuming it

Depend on an exact revision:

```bash
go get github.com/auto-pigeon/auto-pigeon-libraries@<full commit sha>
```

Go records the pseudo-version in `go.mod` and its checksum in `go.sum`; no registry, token or sibling
checkout is involved. To build a consumer against a local checkout, use an uncommitted workspace file
and never a `replace` directive in the committed `go.mod`:

```bash
go work init . ../auto-pigeon-libraries   # go.work stays untracked
```

## Known limitations

These are preserved behaviour, not defects this module quietly fixes:

- **RMF stores what its reader reads, and nothing else.** Visgroups, groups, paths, per-object editor
  colours and cameras are not carried, so opening a written `.rmf` in Worldcraft loses them; the model
  round-trips, the bytes do not. Nothing here rewrites an author's `.rmf` in place.
- **RMF cannot tell an explicit `0 0 0` Quake 2 face tail from none.** Worldcraft's own rule is
  reproduced: all-zero tails are omitted. RMF 1.6 has no surface block at all, so those fields are
  absent rather than zero.
- **RMF coordinates are float32.** A coordinate from `.map` text that a float32 cannot hold returns as
  the nearest six-decimal value. `apmap-to-q2rmf` refuses Valve 220 faces and values RMF cannot hold.
- **Classic UV through SER-7.** A Classic face whose rounded points would move its texture is stored
  as other Classic numbers when they keep its texels (`classic_uv_rebased`). When no Classic spelling
  can, the literal numbers are kept and the face is **named** (`classic_uv_unrepresentable`): a known
  loss, reported, never silent. Geometry is never moved and Valve 220 is never produced.
- **Quake III.** `apmap-to-q3map` refuses, whole and by code, what Q3Map2 would misread; the other
  `.map` writers refuse any patch or brush-primitives face rather than dropping it. A Quake III file
  mixing Classic and `brushDef` brushes imports and cannot be written back as one file
  (`q3map_mixed_file_syntax`: Q3Map2 reads one brush syntax per file). Comments and whitespace of a
  source `.map` are not kept; the written map is a fixed point of read-and-write from the first trip.
- **`.map` output drops `groups` and `authorship`.** The format has no word for them.

## Provenance

`PROVENANCE.md` records where this code came from and the third-party material it carries.
