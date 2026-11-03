# @auto-pigeon/mapio

Quake `.map` and APMap IO for TypeScript — the reader, writer and validator half of the APMap toolchain,
for browsers, workers and Node. Apache-2.0.

It is the TypeScript sibling of the Go library [`go/mapio`](../../go/mapio/): the same formats for a
different runtime. Both implement the contract in [`@auto-pigeon/apmap-schema`](../apmap-schema/) and
neither embeds a copy of it.

## What it does — and what it does not

| route | direction | game | what is kept exactly | named refusals |
| --- | --- | --- | --- | --- |
| `parseQuakeMap` → `apmapFromParsedMap` | `.map` → APMap | `quake1` (default) | Classic and Valve 220 texture mapping (the dialect is inferred), property order and repeated keys, source identity | `ApmapEncodeError` (`apmap_encode_*`): unreadable brush, bad texture name, bad id |
| same, `{ game: 'quake2' }` | `.map` → APMap | `quake2` | every face tail exactly — an explicit `0 0 0` is not an absent tail | as above |
| `parseQuake3Map(Async)` → `apmapFromQuake3Map(Async)` | `.map` → APMap | `quake3` | Classic, Valve 220, `brushDef` 2×3 matrices, `patchDef2` control points and UVs, shader names (a `textures/` prefix is stored without it, and counted) | `Quake3ImportError`: Doom 3 grammars, bad patch size, brush epairs, unknown blocks, in-brush mixed syntax, an illegal brush — **the whole file**, never part of it |
| `mapTextFromApmap` | APMap → `.map` | any; `quake2` and `quake3` have their own rules | as stored | `MapExportError` (`map_export_*`): ADM-Q2-1..4 before a byte is written; a Quake III file mixing brush syntaxes; a patch in a non-Quake-III document |
| `parseApmapText` / `decodeApmapDocument` | APMap text → document | all | every member; a legacy document is promoted to the current version at this door | `ApmapDecodeError`: unreadable JSON, a version the contract does not hold |
| `canonicalApmapText` | document → APMap text | all | SER-7 / SER-7Q numbers, member order | `ApmapEncodeError` |
| `inspectApmapText` | on demand | all | — | returns issues: decode, schema (the schema matching the document's own version), semantic (`SEM-*`, groups, Quake III surfaces, identity) |
| `analyseApmapIdentity` / `repairApmapIdentity` | on demand | all | ids never move; only false/absent `derived_from` is rewritten | `unsupported` when any object does not match a demonstrated signature |

Not here, on purpose:

- **Worldcraft `.rmf`.** Binary, and read/written by Go `go/mapio/rmf` on a server. There is no RMF code in
  this package.
- **Half-Life.** `apmapFromParsedMap` accepts any registry name as `game`, `half_life` included, and
  reads Classic faces by the `qbsp` rule; nothing has been measured against a Half-Life compiler, so that
  is a string accepted, not a game supported.
- **Rendering, editing, UI.** Triangulation, patch tessellation, transforms, the editor's scene and its
  change sets stay with the editor. `parsedMapFromApmap` is a reader's view of the brushes for a
  consumer that draws; it is not whole-document IO and never a way to save one.

Known, documented loss: a Quake III `.map` mixing Classic and `brushDef` brushes imports and cannot be
exported as one file (Q3Map2 reads one brush syntax per file); a plane that bounds nothing is left out of
the document and listed in the import summary (`redundantPlaneBrushes`); an exact duplicate plane is kept
and reported as a `duplicate_plane` risk.

## The contract is the host's to supply

Nothing in this package names an APMap version. The host tells it which version it writes and which it
reads, from `@auto-pigeon/apmap-schema`'s **filenames** — the one schema directly under `schema/` is
current, each one under `schema/deprecated/` is a readable legacy contract:

```js
import { readdirSync, readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { setCurrentApmapVersion } from '@auto-pigeon/mapio';

const schemaDir = dirname(createRequire(import.meta.url).resolve('@auto-pigeon/apmap-schema'));
const NAME = /^apmap-(\d+\.\d+)\.schema\.json$/;
const [current] = readdirSync(schemaDir).filter((n) => NAME.test(n));
const legacy = readdirSync(join(schemaDir, 'deprecated')).filter((n) => NAME.test(n));
const schemas = Object.fromEntries([[current, schemaDir], ...legacy.map((n) => [n, join(schemaDir, 'deprecated')])]
  .map(([n, dir]) => [NAME.exec(n)[1], JSON.parse(readFileSync(join(dir, n), 'utf8'))]));
setCurrentApmapVersion(NAME.exec(current)[1], Object.keys(schemas));
```

Until that call, every APMap read and write refuses — a default would be a second place the version is
written down. Validation is a separate, explicit call (`inspectApmapText(text, { schema, schemas })`);
nothing here validates on load, on save or on a timer. In a page whose Content-Security-Policy forbids
`unsafe-eval`, `ajv` cannot compile a schema: generate validators at build time and register them with
`configureApmapSchemaValidators` (each is matched to its schema by `apmapSchemaDigest`).

## Examples

```js
import { apmapFromParsedMap, canonicalApmapText, mapTextFromApmap, parseQuakeMap } from '@auto-pigeon/mapio';

const parsed = parseQuakeMap(mapText);                     // { entities, errors }
if (parsed.errors.length) throw new Error(parsed.errors[0].message);
const document = apmapFromParsedMap(parsed, { game: 'quake2', sourceMap: 'base1.map', sourceText: mapText });
const apmapText = canonicalApmapText(document);            // what a store keeps
const back = mapTextFromApmap(document);                   // what a compiler reads
```

```js
import { apmapFromQuake3MapAsync, parseQuake3MapAsync, sha256Hex, Quake3ImportError } from '@auto-pigeon/mapio';

const controller = new AbortController();
const parsed = await parseQuake3MapAsync(text, { signal: controller.signal, onProgress: (done, total) => {} });
try {
  const { document, summary } = await apmapFromQuake3MapAsync(parsed, {
    sourceMap: 'arena.map', sourceSha256: sha256Hex(text), signal: controller.signal });
} catch (error) {
  if (error instanceof Quake3ImportError) { /* refused whole: error.code, error.message */ }
  if (error?.name === 'AbortError') { /* cancelled: nothing was produced */ }
}
```

## Entry points and packaging

- `@auto-pigeon/mapio` — the curated surface above (`src/index.ts`).
- `@auto-pigeon/mapio/<dir>/<module>` — any one module (`apmap/types`, `map/quake3`, `geometry/brushLegality`,
  …), for a consumer that wants exactly one. The root re-exports the same objects, so an error class is
  one class whichever entry it came from.

The source is TypeScript with explicit `.ts` relative imports. `npm pack` runs `prepack` → `tsc` and ships
`dist/` (ES2022 modules and `.d.ts`): that is what an installed consumer gets by default, in Node or a
bundler. A consumer that compiles TypeScript itself can ask for the source with the `auto-pigeon-source`
export condition. Runtime dependency: `ajv` (the run-time schema compile in Node). No Three.js, no DOM
tree, no network.

## Who uses it

`used-by.json`. Auto-Pigeon's editor consumes a byte-identical copy of this package (its Docker build cannot
reach a sibling checkout); a test there fails when the copy drifts.

## Tests

```sh
npm test          # typecheck + vitest: every route on authored fixtures, every apmap-schema vector, the boundary
npm run build     # dist/
```

`test/fixtures/` are authored for these tests (Apache-2.0, no content from any other map).
