---
id: aulibs.layout
schema: aut-agent-module/1
repository: auto-pigeon-libraries
title: Layout — one folder per package, and what each one must contain
authority:
  - aulibs-layout
topics:
  - layout
  - packages
  - folders
  - fixtures
  - readme
  - schema
  - structure
paths:
  - ts/**
  - go/**
  - fixtures/**
  - meta/**
---

# Layout — one folder per package, and what each one must contain

<!-- Moved verbatim from AGENTS.md by NEW_246G_AUT_AUG_AUCOM_AUTEL_AULIBS_Remaining-Repository-Documentation-Modularization: line(s) 178-204 of the AGENTS.md at sha256 5431a0e7eea7acc0. This module is the ONE authoritative home for the rules below; the repository-root AGENTS.md routes to it and keeps no second copy. -->

## 4. Layout

```text
auto-pigeon-libraries/
├── LICENSE                  # Apache-2.0
├── README.md
├── AGENTS.md                # this file — the authority
├── CLAUDE.md
├── run.sh                   # ./run.sh test
├── meta/
│   └── used-by.schema.json  # JSON Schema for every package's used-by.json
├── fixtures/                # apmap fixtures, shared across packages
├── ts/                      # one folder per TypeScript package
└── go/                      # one folder per Go library, in the one module at the root
```

Rules:

- **One folder per package** under `ts/` and `go/`. No nesting packages inside
  packages, no shared-source folder that several packages reach into.
- **A Go library is one folder under `go/`, and may contain ordinary Go packages.**
  `go/mapio` is the library; `go/mapio/apmap` and the rest are its Go packages and
  `go/mapio/internal/` its private ones. They share the library's README and
  `used-by.json`, because they version, ship and are consumed together. This is
  a statement about Go packages inside one library, never a licence to nest an
  independent library, and an independent library still gets its own folder and
  its own consumer manifest. All of it is one Go module at the repository root,
  with no nested `go.mod`, so `./run.sh test` reaches it.
- **Every package folder contains its own `used-by.json`**, validating against
  `meta/used-by.schema.json`, and its own `README.md` saying what the package
  is and how to use it.
- `fixtures/` is shared and has its own conventions — see `fixtures/README.md`.
  Read it before adding a fixture; the conventions were agreed before the first
  fixture existed precisely so they would hold.
