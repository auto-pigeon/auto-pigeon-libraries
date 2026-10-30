---
title: "Adding Quake II sample maps broke the editor backend, which refused the game"
author: Andrea-dintino
date: 2026-10-30
standing: true
id: l-d9405afe
scope: repo
status: active
src: init-sessions
---

## Watch out
- Consumer suites mostly accepted the new vectors, but the AUP backend rejected every one with an unsupported-game error. Adding contract vectors needs a sweep of every consumer's tests.
- In the code: Fixed in auto-pigeon backend/auto_pigeon/domain/apmap.py (apmap_game_unsupported for quake2). After adding vectors in AULIBS, run each consumer's conformance suite (AUP, AUB, AUC, AUE).

_From past Claude sessions here, Oct 13._
