---
id: aulibs.consumption
schema: aut-agent-module/1
repository: auto-pigeon-libraries
title: How a consumer depends on a package — settled for development, open for deployment
authority:
  - aulibs-consumption-mechanism
  - aulibs-docker-build-context
  - aulibs-no-dockerfile
topics:
  - consumption
  - dependency
  - file-path
  - npm
  - go-module
  - publication
  - docker
  - build-context
  - vendoring
  - dockerfile
paths:
  - package.json
  - go.mod
---

# How a consumer depends on a package — settled for development, open for deployment

<!-- Moved verbatim from AGENTS.md by NEW_246G_AUT_AUG_AUCOM_AUTEL_AULIBS_Remaining-Repository-Documentation-Modularization: line(s) 205-231, 244-252 of the AGENTS.md at sha256 5431a0e7eea7acc0. This module is the ONE authoritative home for the rules below; the repository-root AGENTS.md routes to it and keeps no second copy. -->

## 5. Consumption mechanism — partially open

How a consumer depends on a package here is **settled for development and open
for deployment**. Stated honestly rather than presented as finished:

- **Preferred:** a local-path dependency (`file:../auto-pigeon-libraries/ts/<pkg>`
  or a Go `replace` directive). Simplest, and it makes a change visible to its
  consumer immediately.
- **Fallback:** a git-URL dependency, pinned to a commit.
- **Deferred:** npm publication. Not done, not scheduled, and not to be
  introduced as a side effect of another task.
- **Go: an exact revision of the public module.** Since `NEW_371` the module at the
  repository root is consumed the way any public Go module is: `go.mod` pins a
  pseudo-version of a real commit and `go.sum` its checksum. No registry, no
  token, no tag is needed, and `go/mapio/README.md` shows the commands. A
  committed `replace` pointing at a sibling checkout is not allowed, because a
  clean checkout of the consumer could not build: local development uses an
  untracked `go.work`.

### Known unsolved issue: Docker build context

**A local-path dependency points outside a consuming repository's Docker build
context.** `docker build` cannot see `../auto-pigeon-libraries/`, so a consumer
that works locally will fail to build its image, and it will fail at image-build
time rather than at development time — the least convenient moment to discover
it.

This is written down so no one trips on it silently. **The decision about how a
given repository's `docker build` resolves the dependency** — a larger build
context, a vendoring step, a git-URL dependency for the image only, or
publication — **belongs to the adoption prompt in that consuming repository, not
to this repository.** The first adoption in each repo makes that call and
records it there.


## 7. No Dockerfile

This repository ships libraries, not a service. The standing workspace rule
that a repository must be Docker-deployable has a precondition — a deployable
image — and that precondition does not hold here.

**Do not add a Dockerfile to this repository out of habit.** If a task appears
to need one, the need belongs to a consuming repository (§5).
