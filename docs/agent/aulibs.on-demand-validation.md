---
id: aulibs.on-demand-validation
schema: aut-agent-module/1
repository: auto-pigeon-libraries
title: Validation is on-demand only — nothing here wakes up and declares a map broken
authority:
  - aulibs-on-demand-validation
topics:
  - validation
  - on-demand
  - api-surface
  - errors
  - exceptions
  - product-rule
paths:
  - ts/offline-workspace-contract/**
  - ts/incident-contract/**
  - go/mapio/**
---

# Validation is on-demand only — nothing here wakes up and declares a map broken

<!-- Moved verbatim from AGENTS.md by NEW_246G_AUT_AUG_AUCOM_AUTEL_AULIBS_Remaining-Repository-Documentation-Modularization: line(s) 119-135 of the AGENTS.md at sha256 5431a0e7eea7acc0. This module is the ONE authoritative home for the rules below; the repository-root AGENTS.md routes to it and keeps no second copy. -->

### 2.1 Validation is on-demand only

Library functions may check maps — schema, convexity, holes, anything — **when
called**. Nothing in AULIBS may be designed to run automatically: not at load
time, not at save time, not on a timer, not in a background worker, not as a
side effect of a constructor or an import.

The reason is a fact about how the product is used. Levels are routinely
"broken" simply because someone is still working on them; a half-carved brush
is a normal intermediate state, not an error. **No component ever wakes up and
declares a map broken.** A caller who wants a verdict asks for one and decides
what to do with it.

This shapes the API surface, not just the scheduling: a validation result is a
returned value, never a thrown exception on a load path, never a log line
emitted from a code path the caller did not ask to validate.
