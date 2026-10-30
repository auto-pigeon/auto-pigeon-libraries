---
title: Check that a behaviour-parity harness masks per-run timings before trusting its differences
author: Andrea-dintino
date: 2026-10-30
standing: true
id: l-05b633b7
scope: repo
status: active
src: init-sessions
---

## Watch out
- The first parity run reported 2,618 differences out of 3,473 cases. Most were the harness failing to mask a per-run duration field in stdout, while two cases were real.
- In the code: In the NEW_371 parity harness, mask total_duration_ms in stdout before comparing against the baseline binary. Then investigate the remaining differences, especially exit-code changes.

_From past Claude sessions here, Oct 30._
