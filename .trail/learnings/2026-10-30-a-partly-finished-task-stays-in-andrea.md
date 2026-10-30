---
title: A partly finished task stays in the queue and is not archived as done
author: Andrea-dintino
date: 2026-10-30
standing: true
id: l-7c523b2c
scope: repo
status: active
src: init-sessions
---

## Decided
- Moving a prompt to done/ marks it complete for the runner. The headed-browser journey still failed on defects owned by other work, so the prompt was left in place with status partial.
- In the code: Move prompts to done/ only when fully finished. For partial work, set the handoff header to status: partial and leave the prompt in $MAPPER_ROOT/LLM/prompts/auto-pigeon/.

_From past Claude sessions here, Oct 13._
