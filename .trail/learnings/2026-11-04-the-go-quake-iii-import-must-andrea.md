---
title: "The Go Quake III import must copy the TypeScript arithmetic exactly, and consumers can pin an unpushed library commit"
author: Andrea-dintino
date: 2026-11-04
branch: main
id: l-1d4c7cb4
scope: repo
status: active
src: agent
touches:
  - go/mapio/legality/arith.go
  - go/mapio/apmap/quake3import.go
  - go/mapio/quake3map/decode.go
---

## Decided
- Go `q3map-to-apmap` (NEW_389) is a transliteration of ts/mapio's quake3.ts + quake3Encode.ts + brushLegality/convexity/reconstructBrush/quakeWinding, not a redesign: admission compares computed values with tolerances, so one ulp decides whether a plane is idle or a brush is refused.
- Parity is canonical bytes except provenance.producer/producer_version (Go names itself). Identity and document_id follow the TS importer (`<stem>:full_map`), not go/mapio's Q1 `DocumentID`.

## Watch out
- SER-7 in the Q3 importer is JavaScript's `Math.round(v*1e6)/1e6` (jsSER7), NOT go/mapio's decimal round-half-even `ser7`; Encode then finds the values already canonical.
- `Math.hypot` is V8's scaled Kahan sum, not sqrt(x²+y²+z²) — they differ in the last bit (0.7071067811865476,0.7071067811865476,0 → 1.0000000000000002). legality.Hypot reproduces it.
- Wrap every product in float64() before adding: the arm64 Go compiler fuses x*y+z into FMADD. Check with `GOOS=darwin GOARCH=arm64 go build -gcflags=-S ./go/mapio/legality | grep FMADD` (0 expected).
- The reconstruct candidate sort is V8's one comparison comparator(positive, negative); two infinite extents give NaN and fall to the sign.
- Text is the WHATWG decode (BOM dropped, maximal-subpart U+FFFD); columns count UTF-16 units.
- The fixtures alone did not catch a flipped winding sign: only >64-face brushes reach the winding report. q3-many-faces.map exists for that.

## Tried and ruled out
- Pinning a consumer to a local-only AULIBS commit with a committed `replace` (forbidden). Instead resolve the real pseudo-version from the local repo: GOPRIVATE=github.com/auto-pigeon/auto-pigeon-libraries GOPROXY=direct GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=url./path/to/auto-pigeon-libraries.insteadOf GIT_CONFIG_VALUE_0=https://github.com/auto-pigeon/auto-pigeon-libraries GIT_CONFIG_KEY_1=protocol.file.allow GIT_CONFIG_VALUE_1=always go get ...@<sha>. The go.sum h1 is the one the proxy computes once pushed; nothing else can build it until then.

In the code: go/mapio/legality/arith.go, go/mapio/apmap/quake3import.go (jsSER7), go/mapio/quake3map/decode.go, ts/mapio/test/fixtures/quake3-conformance/ (MAPIO_WRITE_QUAKE3_REFERENCE=1 regenerates).
