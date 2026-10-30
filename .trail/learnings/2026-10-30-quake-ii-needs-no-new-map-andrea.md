---
title: Quake II needs no new map format version; the current one is enough
author: Andrea-dintino
date: 2026-10-30
standing: true
id: l-8ce47cd5
scope: repo
status: active
src: init-sessions
---

## Decided
- Q2_001 checked whether Quake II maps need a new schema version. Adding Quake II sample maps showed the existing 1.5 schema carries them, with no schema byte or frozen digest changed.
- In the code: Do not bump the APMap schema for Quake II. Q2 vectors live in auto-pigeon-libraries/ts/apmap-schema/test-vectors (valid/q2-*, quake2-admission/); rules are in SEMANTICS.md §6.3a.

_From past Claude sessions here, Oct 13._
