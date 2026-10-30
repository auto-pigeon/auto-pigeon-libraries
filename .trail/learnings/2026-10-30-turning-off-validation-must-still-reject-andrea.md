---
title: Turning off validation must still reject maps from a newer format version
author: Andrea-dintino
date: 2026-10-30
standing: true
id: l-a1a43134
scope: repo
status: active
src: init-sessions
---

## Watch out
- A future-version map was converted through the validation-off path as if it were current. The version check is now always on.
- In the code: In AUE, `--validate-off` skips only the schema and SEM layers, never the version gate (internal/convert/convert.go, internal/cli/convert.go).

_From past Claude sessions here, Oct 13._
