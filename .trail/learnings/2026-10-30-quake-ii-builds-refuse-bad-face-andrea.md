---
title: Quake II builds refuse bad face tails and paths escaping the game root
author: Andrea-dintino
date: 2026-10-30
standing: true
id: l-4276255d
scope: repo
status: active
src: init-sessions
---

## Decided
- Compiling authored tail variants showed real compilers crash, silently truncate or behave differently per compiler. So the converter now rejects those documents before writing any output instead of rewriting them.
- In the code: AUE apmap-to-q2map applies ADM-Q2-1..4 in internal/apmap/quake2admission.go (typed Quake2AdmissionError, codes q2map_*, CLI reason schema_invalid). An absent tail is admitted. apmap-to-q1map is unchanged. Never rewrite the document.

_From past Claude sessions here, Oct 13._
