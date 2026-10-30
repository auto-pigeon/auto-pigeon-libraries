# Go packages

Go libraries live here, in ONE Go module whose `go.mod` is at the repository root:
`github.com/auto-pigeon/auto-pigeon-libraries`.

| package | what it is |
| --- | --- |
| [`mapio`](mapio/) | converts and validates `.map`, APMap and Worldcraft `.rmf` |

Each folder directly under `go/` follows the same rules as `ts/`: its own
`README.md` and its own `used-by.json` validating against
`../meta/used-by.schema.json`, and **no package without tests**. A library may
split its implementation into ordinary Go packages below its own folder; they
share that folder's README and manifest.

Do not add a nested `go.mod`: `./run.sh test` runs `go test ./...` from the
repository root, and a nested module would silently fall outside it.

`AGENTS.md` is the authority: §2 for the two product rules (validation is
on-demand only; every package declares its consumers), §3 for the `used-by.json`
protocol, §5 for how consumers depend on a package.
