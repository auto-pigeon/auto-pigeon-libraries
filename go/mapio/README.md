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
| `apmap-to-q3map` | APMap (`quake3`) | Quake III `.map` plus a manifest | `quake3` |

There is **no** Quake III `.map` importer here, and RMF converts only through APMap.

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
`quake3_surface`, `cancelled`, `input_too_large` or `invalid_input`. The original typed error stays
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
  `.map` writers refuse any patch or brush-primitives face rather than dropping it.
- **`.map` output drops `groups` and `authorship`.** The format has no word for them.

## Provenance

`PROVENANCE.md` records where this code came from and the third-party material it carries.
