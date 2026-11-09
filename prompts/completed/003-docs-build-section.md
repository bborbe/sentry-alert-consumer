---
status: completed
spec: [001-cluster-build-arm]
summary: Added a Build section to README.md documenting the local and cluster arms, mirror coupling, and parse-time traps, plus a cluster build bullet under CHANGELOG.md's Unreleased heading
execution_id: 03747306-83e4-562c-aa21-2038ffefaab8-exec-003-docs-build-section
dark-factory-version: dev
created: "2026-11-09T20:07:41Z"
queued: "2026-11-09T20:36:49Z"
started: "2026-11-09T20:36:50Z"
completed: "2026-11-09T20:37:49Z"
branch: dark-factory/cluster-build-arm
---

<summary>
- Documents the two build arms in the README.
- Shows the exact cluster invocation, including the required commit and tag arguments.
- States that the publish-all target and its upload and clean steps stay local.
- Explains that the cluster pushes Docker Hub and that the private registry copy is produced by the nuke mirror step.
- Records the two parse-time makefile traps that the implementation depends on.
- Adds a changelog entry under the unreleased heading.
- Does not change any build logic or the lane configuration.
</summary>

<objective>
Add a build section to the README that documents the local and cluster arms, the mirror coupling, and the two parse-time makefile traps, and add a changelog bullet under `## Unreleased` recording the change.
</objective>

<context>
Read these files before editing:
- `README.md` — it has no build section today; headings are `How It Works`, `Configuration`, `Observability`, and `Task Contract`.
- `CHANGELOG.md` — it already has an `## Unreleased` section; add the bullet there.
- `Makefile` — note that line 3 includes `Makefile.docker` before line 5 assigns `SERVICE`.
- `Makefile.docker` — the dispatch being documented.
- `docs/dod.md` — the project definition of done.

Reference documentation (in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/documentation-guide.md`
</context>

<requirements>
1. Add a `## Build` section to `README.md` after `## How It Works` and before `## Configuration`. The section must cover both arms, the mirror coupling, and the two parse-time traps. Use this text as the target content (adapt wording only if necessary; keep the literal strings the verification greps for):

   ~~~~markdown
   ## Build

   The image can be built on this machine or inside a Kubernetes cluster. Both
   arms build the same `Dockerfile` and publish to the same repository,
   `docker.io/bborbe/sentry-alert-consumer`, differing only in tag.

   ### Local arm (default)

   With no `BUILD_TARGET` set, `make build` runs `docker build` on this machine
   and tags the image `docker.io/bborbe/sentry-alert-consumer:$(VERSION)`. The
   command and the tag are unchanged. `make buca` (and its `upload` / `clean`
   steps) stays local-only: it pushes from this machine and removes the local
   image.

   ### Cluster arm

   ```bash
   make build BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>
   ```

   `BUILD_TARGET` names a cluster alias (`sun`, `nukedev`, `nukeprod`) or a
   kubectl context. `REF` must be a full 40-character commit SHA that exists on
   `origin`. `VERSION` must be supplied by the caller: the cluster arm refuses
   to run when `VERSION` would resolve from the makefile's own `git describe`
   default, so a released tag cannot be republished by accident.

   The arm renders a Kubernetes Job from `hack/build-job.yaml`, applies it
   through `hack/build-cluster.sh`, waits for it, and reports the Job's log
   lines. It pushes `docker.io/bborbe/sentry-alert-consumer:<tag>` to Docker
   Hub. The private registry copy the deployment pulls from is produced by
   nuke's `MIRROR_IMAGES` step, not by this build.

   Before the first cluster build, provision the `build` namespace and the
   `buildkit-push` credential once:

   ```bash
   hack/build-cluster-bootstrap.sh <cluster>
   ```

   ### Two parse-time traps

   - `VERSION` is defaulted at parse time in `Makefile.docker` before any guard
     can read its origin. The cluster arm captures the origin before that
     default and refuses a defaulted tag; a guard written after the default
     would never fire.
   - `Makefile` includes `Makefile.docker` before it assigns `SERVICE`, so
     `$(SERVICE)` may appear only in recipes or recursively-expanded variables.
     A `:=` or an `ifeq` on it evaluates empty and composes
     `docker.io/bborbe/:tag`.
   ~~~~

   Do not copy the outer `~~~~markdown` fence into the README; it only delimits the suggested content in this prompt. The nested `bash` fences are part of the README content.

   The literal invocation `make build BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>` must appear verbatim in the README.

2. State in the build section that `buca` (and its `upload` / `clean` steps) remains local-only. The word `buca` must appear in the README.

3. State in the build section that the cluster arm pushes Docker Hub while nuke's `MIRROR_IMAGES` step produces the private copy. The literal `MIRROR_IMAGES` must appear in the README. Do not claim the cluster arm pushes to the private registry.

4. Record both parse-time traps in the build section: `VERSION` is defaulted before any guard can read its origin, and `Makefile.docker` is included before `SERVICE` is assigned.

5. Add one bullet under the existing `## Unreleased` heading in `CHANGELOG.md`. The bullet must contain the exact lowercase phrase `cluster build`. Use this wording:

   ~~~~markdown
   - feat: cluster build arm — `make build BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>` builds and pushes the image inside the target cluster; the local arm and `buca` are unchanged
   ~~~~

   Do not copy the outer `~~~~markdown` fence into the changelog. Keep every existing bullet in `## Unreleased` and every released section unchanged.

6. Do not modify `.dark-factory.yaml`. It is pre-landed and already carries the measured-working shape: `workflow: direct`, `pr: false`, `defaultBranch: master`, `autoMerge: false`, `autoRelease: false`, `autoGeneratePrompts: true`, and `validationPrompt: docs/dod.md`.

7. Do not modify `Makefile`, `Makefile.docker`, `Dockerfile`, or anything under `hack/` in this prompt. This prompt is documentation only.

8. Do not add a mirror entry or bump the pin in `bborbe/nuke`. Mentioning the mirror in the README is the only nuke-related change.
</requirements>

<constraints>
- `README.md` gains the build section: it does not exist today, so this change makes it the in-repo home for the two-arm topology, the mirror coupling, and the two parse-time traps.
- The documented build path must match reality: the cluster invocation is `make build BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>`, `buca` remains local, and the cluster pushes Docker Hub while nuke's mirror produces the private copy.
- The CHANGELOG bullet must sit under `## Unreleased`, and the phrase `cluster build` must be on that bullet line.
- Do not modify `.dark-factory.yaml`; its values are pre-landed and pinned.
- Do not touch the sibling repositories or `bborbe/nuke`.
- No new scenario. The behaviour is reachable by checks on the rendered invocation plus one real cluster build.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
</constraints>

<verification>
Run these from the repository root; all are container-executable.

```bash
# The exact cluster invocation is documented.
test "$(grep -c 'BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>' README.md)" -ge 1

# buca is documented as local-only.
test "$(grep -ci 'buca' README.md)" -ge 1

# The mirror coupling is documented.
test "$(grep -c 'MIRROR_IMAGES' README.md)" -ge 1

# The changelog bullet is under Unreleased.
awk '/^## /{s=$0} /cluster build/{print s}' CHANGELOG.md | grep -qx '## Unreleased'

ROOTDIR="$PWD" make precommit
```
</verification>
