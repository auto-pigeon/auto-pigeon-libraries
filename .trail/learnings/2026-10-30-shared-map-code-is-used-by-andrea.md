---
title: Shared map code is used by AUE through a pinned library commit with no sibling checkout
author: Andrea-dintino
date: 2026-10-30
standing: true
id: l-a143ccd7
scope: repo
status: active
src: init-sessions
---

## Decided
- The map reader and writer moved from the extractor into the shared libraries repo. Clean builds, including Docker, must work without a sibling checkout, so they were tested against a pinned commit through a real module proxy.
- In the code: AUE pins auto-pigeon-libraries by commit (go get ...@$REV). Verify with GOWORK=off, an empty module cache and a real `docker build`. Set GOPRIVATE='github.com/auto-pigeon/*' for tidy.

_From past Claude sessions here, Oct 30._
