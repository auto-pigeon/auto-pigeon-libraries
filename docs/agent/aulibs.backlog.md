---
id: aulibs.backlog
schema: aut-agent-module/1
repository: auto-pigeon-libraries
title: Deferred work is a GitHub Issue; the historical backlog is read-only evidence
authority:
  - aulibs-backlog-duties
topics:
  - github-issues
  - backlog
  - deferred-work
  - deferral
  - follow-up
  - next-task
---

# Deferred work is a GitHub Issue; the historical backlog is read-only evidence

<!-- Moved verbatim from AGENTS.md by NEW_246G_AUT_AUG_AUCOM_AUTEL_AULIBS_Remaining-Repository-Documentation-Modularization: line(s) 58-90 of the AGENTS.md at sha256 5431a0e7eea7acc0. Superseded by NEW_397 (HITL): the append and delete duties are retired. -->

## 0a. The backlog — where deferred work lives

**Deferred work is a GitHub Issue** — HITL, `NEW_397`. The binding rule is the root `AGENTS.md`
item marked `github-issue-policy/1`; the one detailed policy is AUT
`docs/agent/aut.github-issues.md`, and AUT `scripts/issue_policy.py` derives the target repository
from this checkout's `origin`, checks Issues and permission, and searches open and closed issues.

- **The historical backlog is read-only.** `$MAPPER_ROOT/LLM/backlog/` — this repository's
  `backlog/auto-pigeon-libraries.md`, `workspace.md` and every other file there — is evidence. Read it at task
  start: an entry may describe the defect you are about to file or the work your prompt
  supersedes, and the issue or handoff cites it. Never append, delete, reorder or annotate it; the
  duties *append on deferral* and *delete on adoption* are retired. HITL may still prune it.
- **A `Next Recommended Task` that defers work names the issue it created or extended**, or the
  `GitHub issues not published` draft when GitHub could not be written.
