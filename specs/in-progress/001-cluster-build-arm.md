---
status: approved
approved: "2026-11-09T18:15:17Z"
branch: dark-factory/cluster-build-arm
---

## Summary

- This repo's image can currently be produced on one machine only: the Mac.
- Its sibling repos can also build their image inside a Kubernetes cluster, unattended, from any committed ref.
- Add that second build path here as an arm beside the existing local one, leaving the local command and tag as they are.
- The cluster arm builds a named commit as a Kubernetes Job and pushes the result to `docker.io/bborbe/sentry-alert-consumer`, the repository nuke's mirror step copies into the private registry the deployment pulls from.
- Two supporting edits make the arm reachable: the image must build from a fresh clone with no `vendor/`, and the dark-factory lane config must stop using the shape that is known to lose commits.

## Problem

`sentry-alert-consumer` runs on nukeprod, and `nuke/sentry-alert-consumer/Makefile` mirrors `docker.io/bborbe/sentry-alert-consumer:$(CONSUMER_TAG)` into `docker.prod.nuke.benjamin-borbe.de:443`. Every image that mirror copies is cut by hand on this Mac today: `Makefile.docker` carries only the local `docker build` path, and `hack/` does not exist. Nothing else can produce this image, so a release while the operator is away from this machine cannot happen.

The arm is not a straight copy. Three properties of this repo make a naive paste fail at runtime rather than at review:

- **`VERSION` is defaulted at parse time.** `Makefile.docker` opens with `ifeq ($(VERSION),) VERSION := $(shell git describe --tags …) endif`, so by the time any guard could read `$(origin VERSION)` the variable is `file` and an omitted tag silently resolves to the last released tag. A cluster arm that inherited that default would republish a released tag over itself, irreversibly.
- **`Makefile` includes `Makefile.docker` before it assigns `SERVICE`.** `Makefile:3` is the include and `Makefile:5` is `SERVICE = sentry-alert-consumer`, so any parse-time use of `$(SERVICE)` inside `Makefile.docker` — an `ifeq`, a `:=` — evaluates empty and composes `docker.io/bborbe/:tag`.
- **The image build resolves dependencies from `vendor/`.** The Dockerfile runs `go build -mod=vendor`, `vendor/` is gitignored, and `origin/master` carries no `vendor/` directory. A Job that clones the repo has no `vendor/`, so the build fails before it starts.

A fourth property only bites the lane rather than the build, and it was settled by measurement on this repo rather than by reading the docs. `.dark-factory.yaml` on `origin/master` carries `workflow: worktree` + `pr: true`, and a cluster run on exactly that config failed with `save prompt metadata: open prompts/in-progress/001-spec-001-docs-build-section.md: no such file or directory` — the same signature recorded on `obsidian-redirector` PR #15. The identical spec run under `workflow: direct` + `pr: false` completed all three prompts. So `direct` is the working shape here and `worktree` is the regression, whatever the two older "completed siblings" carry. `autoRelease: false` stays: it keeps the release manual, which this repo wants.

## Goal

`make build` on this repo produces the image locally exactly as it does today, and `make build BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>` produces it inside that cluster as a Kubernetes Job, pushing `docker.io/bborbe/sentry-alert-consumer:<tag>` — the repository nuke's mirror step consumes. Both arms work from the same checkout, and the cluster arm works from a fresh clone with no local preparation.

## Non-goals

- Changing the local arm's command or its tag. Both stay as they are. The shared Dockerfile does move to module resolution, because the cluster arm cannot build from a clone otherwise; the local arm runs the same Dockerfile and is unaffected by how dependencies are fetched.
- Pushing to the private registry from the cluster. The `buildkit-push` credential authenticates against Docker Hub only; the private copy is produced by nuke's `MIRROR_IMAGES` step, not by this build.
- Adding a mirror entry or bumping the pin in `bborbe/nuke`. `CONSUMER_TAG ?= v0.1.0` in `nuke/sentry-alert-consumer/Makefile` is already stale against the deployment's `v0.2.5`; realigning it is a deploy-side change, not this spec's.
- Touching the sibling repos — they already carry their arms.
- Building at release or tag time. This spec adds the capability; *when* a build is triggered is a separate concern. This repo sets `autoRelease: false`, so a green merge cuts no tag.
- Extracting the shared build runner into a common upstream template. The files are copied, not shared.
- Extra build knobs beyond `BUILD_TARGET`, `IMAGE`, `TAG` and `VERSION`. The copy source also carries `ALLOW_DEFAULT_VERSION`, `BUILD_TIMEOUT`, `BUILD_REPO_URL`, `BUILD_DOCKERFILE` and `BUILD_PUSH_SECRET`. `ALLOW_DEFAULT_VERSION` in particular would reintroduce an opt-out on the refusal this spec ships, so it is not copied. The repository URL and the push-secret name are literals in the dispatch, not knobs.

## Alternatives

- **Build in GitHub Actions instead.** Rejected: CI here triggers on push or PR to `master`, so it cannot build an arbitrary committed ref on demand — which is the whole requirement. It would also put a second copy of the Docker Hub credential into GitHub secrets, where one already lives in the cluster's `build` namespace.
- **Fix the local arm instead.** Removing `Makefile.docker`'s parse-time `VERSION` default would make local builds safer, but it changes the tag the deployment already consumes and still leaves the image unbuildable without this Mac. A smaller change that does not reach the goal.
- **Reuse a shared upstream runner.** The five files are copied from `agent-task-executor` and `go-skeleton`, not vendored from a package. A shared template is the right end state, but it is its own change across every repo in the goal's target set; copying first keeps each repo's arm independently verifiable.
- **Do nothing.** See the closing section.

## Acceptance Criteria

- [ ] **The local arm's command and tag are unchanged** — with no `BUILD_TARGET` set, `make -n build` prints the `docker build` recipe and no reference to `hack/build-cluster.sh`; `DOCKER_CONTEXT=orbstack make build` exits 0 and `docker image inspect docker.io/bborbe/sentry-alert-consumer:v0.2.5` exits 0 (`v0.2.5` is what `git describe --tags $(git rev-list --tags --max-count=1)` resolves today, so the tag is spelled out rather than left for the verifier to derive). Evidence: both commands' output, the recorded `docker context show` value, the exit code, and the inspected image id. ⚠️ The `DOCKER_CONTEXT=orbstack` pin is what carries the guarantee, not the ambient state: a docker context pointed at `sun` would make a bare `make build` build on the cluster and prove nothing about the local arm.
- [ ] **Post-Deploy (Rung-2):** the cluster arm builds and pushes from a fresh clone — `make build BUILD_TARGET=sun REF=$(git rev-parse HEAD) VERSION=probe-<timestamp>` exits 0, and `crane digest docker.io/bborbe/sentry-alert-consumer:probe-<timestamp>` resolves to a digest **equal to the build log's `containerimage.digest`**. Evidence: the exit code, the Job name, the build pod's `spec.nodeName`, both digests, and `docker image inspect` on the same reference failing on this Mac with `No such image` — a stopped daemon or a malformed reference does not satisfy this.
  - `deploy_check:` `kubectlsun -n build get secret buildkit-push -o jsonpath='{.metadata.name}'`
  - `deploy_target:` `buildkit-push`
- [ ] **The cluster arm pushes to the repository the deploy consumes** — `make -n build BUILD_TARGET=sun REF=$(git rev-parse HEAD) VERSION=probe` prints `docker.io/bborbe/sentry-alert-consumer:probe` on one line before it calls the runner. Evidence: the printed line alone — the digest half is AC 2's, at AC 2's own tag, so this criterion does not depend on a build that used a different tag.
- [ ] **Post-Deploy (Rung-2):** the cluster arm refuses a tag it did not receive from the caller — `make build BUILD_TARGET=sun REF=$(git rev-parse HEAD)` with no `VERSION` on the command line or in the environment exits non-zero and prints a message naming `VERSION`, before any Job is applied. The refusal fires even though `Makefile.docker` otherwise defaults `VERSION` from `git describe --tags`. Evidence: the exit code, the message, and `kubectlsun -n build get jobs` showing no new Job for that run.
  - `deploy_check:` `kubectlsun -n build get secret buildkit-push -o jsonpath='{.metadata.name}'`
  - `deploy_target:` `buildkit-push`
- [ ] **`hack/` holds the five files and the cluster branch invokes the runner** — at the merge commit on `master`, `git ls-tree --name-only HEAD hack/` lists `build-cluster.sh`, `cluster-alias.sh`, `build-job.yaml`, `build-cluster-bootstrap.sh` and `dockerconfig.json.tpl`, and `grep -cE '^[[:space:]]*hack/build-cluster\.sh' Makefile.docker` returns ≥ 1 on an invocation line rather than a comment. Evidence: the five names and the count. ⚠️ The copied header comments name `BUILD_TARGET`, so a bare `grep -c BUILD_TARGET` passes on a comment alone — the `^` anchor is the probe.
- [ ] **`upload` / `clean` / `buca` stay local, refused at parse time** — `make -n buca` prints `docker push` and `docker rmi`; `make -n buca BUILD_TARGET=sun REF=$(git rev-parse HEAD) VERSION=probe` exits non-zero and prints a message naming those targets as local-only. The refusal is a parse-time `$(error …)`, so it fires under `-n` where no recipe runs. Evidence: both outputs and exit codes.
- [ ] **The clone builds the image without `vendor/`** — the Dockerfile no longer forces `-mod=vendor`, so a Job that clones the repo builds the same way a developer's checkout does; `grep -c '\-mod=vendor' Dockerfile` returns 0. Evidence: that count plus the successful cluster build above, which is the behavioural half. The exact replacement flag is the implementer's call — Go's default `-mod=readonly` resolves modules without letting the build rewrite `go.mod`, so pinning `-mod=mod` would mandate an implementation choice the criterion does not need.
- [ ] **The documented build path and the lane config match reality** — `README.md` shows the exact cluster invocation `make build BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>`, states that `buca` remains local, and records that the cluster pushes Docker Hub while nuke's mirror produces the private copy; `CHANGELOG.md` carries a bullet for this change **under** `## Unreleased`; and `.dark-factory.yaml` sets `workflow: direct` with `pr: false` — the shape that measured working on this repo — plus `defaultBranch: master` and `autoMerge: false`. Evidence: `grep -c 'BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>' README.md` ≥ 1; `grep -ci 'buca' README.md` ≥ 1; `grep -c 'MIRROR_IMAGES' README.md` ≥ 1; `awk '/^## /{s=$0} /cluster build/{print s}' CHANGELOG.md` prints `## Unreleased` (the bare `grep -c '^## Unreleased$'` alone passes at baseline and asserts nothing, and DB 8's bullet is the `cluster build` phrase); and each `.dark-factory.yaml` value is pinned rather than merely present — `grep -c '^workflow: direct$'` = 1, `^pr: false$` = 1, `^autoMerge: false$` = 1, `^autoRelease: false$` = 1, `^autoGeneratePrompts: true$` = 1, `^defaultBranch: master$` = 1 and `^validationPrompt: docs/dod\.md$` = 1, all against `.dark-factory.yaml`. Those read the working tree, so the criterion also requires the change to be **committed**: `git diff --quiet HEAD -- .dark-factory.yaml` exits 0 at the merge commit — otherwise a pathspec commit that skips the config leaves master on the `worktree` shape, and the next spec in this repo hits the same wall.

## Verification

`REF=$(git rev-parse HEAD)` throughout — it is a full 40-character SHA, which is exactly what the runner requires.

### Row 0 — the lane config (pre-landed and committed before approval; not a prompt)

- `grep -c '^workflow: direct$' .dark-factory.yaml` = 1
- `grep -c '^pr: false$' .dark-factory.yaml` = 1
- `grep -c '^defaultBranch: master$' .dark-factory.yaml` = 1
- `grep -c '^autoMerge: false$' .dark-factory.yaml` = 1
- `grep -cE '^(autoRelease: false|autoGeneratePrompts: true|validationPrompt: docs/dod\.md)$' .dark-factory.yaml` = 3
- `git diff --quiet HEAD -- .dark-factory.yaml` exits 0 at the merge commit — every grep above reads the working tree, not the commit

This shape is measured, not copied. On 2026-11-09 the same spec ran twice against PR #25: under `workflow: worktree` + `pr: true` the run failed with `save prompt metadata: open prompts/in-progress/…: no such file or directory`; under `workflow: direct` + `pr: false` all three prompts completed. `pr: true` is also rejected in combination with `workflow: direct`, so the two halves are one decision. The two older "completed siblings" carry `worktree` and predate the regression — do not cite them as the target shape.

### Container-executable — Row 1 (the `Makefile.docker` dispatch)

- `grep -c '^BUILD_TARGET' Makefile.docker` returns ≥ 1
- `grep -cE '^[[:space:]]*hack/build-cluster\.sh' Makefile.docker` returns ≥ 1
- `make -n build` prints a `docker build` recipe and no `hack/build-cluster.sh`
- `make -n build BUILD_TARGET=sun REF=$(git rev-parse HEAD) VERSION=probe` prints `hack/build-cluster.sh` and the composed `docker.io/bborbe/sentry-alert-consumer:probe` line
- `make -n buca BUILD_TARGET=sun REF=$(git rev-parse HEAD) VERSION=probe` exits non-zero naming the local-only targets
- `make -n build BUILD_TARGET=sun REF=$(git rev-parse HEAD)` exits non-zero naming `VERSION`
- `grep -rn 'BUILD_TARGET' --exclude-dir=vendor . | grep -vE '(Makefile\.docker|README\.md|CHANGELOG\.md|hack/|specs/)'` returns 0 lines

### Container-executable — Row 2 (the runner files and the Dockerfile)

- `ls hack/` lists the five expected files — **not** `git ls-tree`, because verification runs pre-commit, when the new files are still untracked and absent from `HEAD`
- `bash -n hack/build-cluster.sh && bash -n hack/cluster-alias.sh && bash -n hack/build-cluster-bootstrap.sh` — the copied runners parse
- `grep -c '\-mod=vendor' Dockerfile` returns 0
- `make precommit` — lint, format and vet clean
- `make test` — unit suites pass

### Container-executable — Row 3 (docs)

- `grep -c 'BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>' README.md` ≥ 1
- `grep -ci 'buca' README.md` ≥ 1
- `grep -c 'MIRROR_IMAGES' README.md` ≥ 1
- `awk '/^## /{s=$0} /cluster build/{print s}' CHANGELOG.md` prints `## Unreleased`

### Operator-executable (runs on the host after PR merge, spec verification ladder)

- `hack/build-cluster-bootstrap.sh sun` — provisions the `build` namespace and the `buildkit-push` secret; idempotent, creates no Job
- `make build BUILD_TARGET=sun REF=$(git rev-parse HEAD) VERSION=probe-<timestamp>` — one real cluster build, Job name, node and both digests recorded
- `crane digest docker.io/bborbe/sentry-alert-consumer:probe-<timestamp>` — the push landed, and equals the build log's digest
- `DOCKER_CONTEXT=orbstack make build` then `docker image inspect docker.io/bborbe/sentry-alert-consumer:v0.2.5` — the local arm still works

## Desired Behavior

1. **A build target selects the arm.** `BUILD_TARGET` defaults to `local`. With `local`, `make build` behaves exactly as it does today: it prepares the module cache, it runs `docker build` on this machine, and it tags `$(DOCKER_REGISTRY)/bborbe/$(SERVICE):$(VERSION)`.
2. **Any other value routes to a cluster Job.** With `BUILD_TARGET` set to a cluster alias or kubectl context, `make build` prints the composed image reference, then renders the Job template and applies it through the copied runner, passing the repository URL and the push-secret name as literals. It waits for the Job and reports success or failure with the Job's own log lines.
3. **The cluster arm needs a caller-supplied tag.** The cluster arm refuses to run unless `VERSION` came from the command line or the environment. It refuses in particular when `VERSION` would resolve from `Makefile.docker`'s own `git describe` default, so a tag nobody chose can never be pushed. The refusal names the variable and happens before any Job is created.
4. **Both arms publish to one repository.** The cluster arm uses `$(DOCKER_REGISTRY)/bborbe/$(SERVICE):$(VERSION)` — the same repository the local arm pushes and nuke's mirror copies, differing only in tag, and composed once so the two cannot drift.
5. **`upload`, `clean` and `buca` remain local-only.** Given a cluster `BUILD_TARGET` they are refused at parse time, by name, before any recipe runs.
6. **The cluster build works from a clone.** The image build resolves its Go dependencies in module mode, so a Job that clones the repo with no `vendor/` directory builds the same way a developer's checkout does. The local arm's `check-go-mod` preparation stays on the local branch only: the cluster arm builds a named ref, which need not be the local `HEAD`.
7. **The lane config is the shape that measured working on this repo.** `.dark-factory.yaml` sets `workflow: direct` with `pr: false` — the pair under which a cluster run completed all three prompts here, where `worktree` + `pr: true` failed at prompt-metadata write. `autoRelease: false` keeps the release manual, `autoGeneratePrompts: true` fires the generator, `autoMerge: false` leaves the merge to the reviewer, `defaultBranch: master` names the base and the `validationPrompt: docs/dod.md` gate stays. This change is **pre-landed and committed on the branch before the spec is approved**, so the lane is correct before the agent is ever asked to run.
8. **The documented build path matches reality.** The README's build section shows both arms, says `buca` stays local, and records why the cluster pushes Docker Hub while nuke's mirror produces the private copy. The CHANGELOG records the change.

## Constraints

- The local arm's command and its tag must not change.
- `VERSION`'s explicitness must be captured **before** `Makefile.docker`'s existing default assignment. After that `ifeq` block the variable's origin is `file` on every invocation, so a guard written below it never fires.
- `$(SERVICE)` may appear only in recipes or recursively-expanded (`=`) variables. `Makefile:3` includes `Makefile.docker` before `Makefile:5` assigns `SERVICE`, so a `:=` or an `ifeq` on it evaluates empty.
- The local-only refusal for `upload` / `clean` / `buca` is a `$(error …)` keyed on the goal list inside the non-local branch, not a recipe-level `exit 1`. A recipe-level refusal is invisible to `make -n`, so the AC could not observe it.
- The five `hack/` files are copied unmodified from `origin/master`: `hack/build-cluster.sh`, `hack/cluster-alias.sh` and `hack/build-job.yaml` from `agent-task-executor` (its Job spec carries `automountServiceAccountToken: false`, which `go-skeleton`'s does not), and `hack/build-cluster-bootstrap.sh` plus `hack/dockerconfig.json.tpl` from `go-skeleton`. Both repositories are public, so a `git clone --depth=1` from inside the container reaches them. `cluster-alias.sh` is required — the runner sources it and fails at that line without it.
- The Job template already passes `DOCKER_REGISTRY=docker.io` as a build argument, so the Dockerfile's base images resolve from the public registry inside the cluster rather than from the private mirror. Do not change that back to the mirror.
- `IMAGE`, `SERVICE`, the registry and the tag must each satisfy the runner's character whitelist; no value may carry a newline.
- `DOCKER_REGISTRY` keeps its `docker.io` default, so the composed reference stays `docker.io/bborbe/sentry-alert-consumer`. No `CLUSTER_IMAGE` variable is needed: unlike the pilot, this repo already composes the `bborbe/`-prefixed path the cluster lane pushes.
- `IMAGE` and `TAG` are defined explicitly rather than left to the implementer to invent: `IMAGE = $(DOCKER_REGISTRY)/bborbe/$(SERVICE)` and `TAG = $(VERSION)`, both recursively expanded (`=`), because the runner consumes repository and tag as two separate variables and `$(SERVICE)` is unset at parse time. The dispatch passes both.
- `.dark-factory.yaml` keeps every other key verbatim — `autoRelease: false`, `autoGeneratePrompts: true`, `validationPrompt: docs/dod.md`. The change is exactly two lines, `workflow` and `pr`; a rewrite that drops the validation gate is a regression.
- `README.md` gains the build section: it does not exist today (headings are `How It Works`, `Configuration`, `Observability`, `Task Contract`), so this spec makes it the in-repo home for the two-arm topology, the mirror coupling, and the two parse-time traps (`VERSION` defaulted before any guard can read its origin; `Makefile.docker` included before `SERVICE` is assigned). The spec dies after implementation; the README does not.
- No new scenario. The behaviour is reachable by checks on the rendered invocation plus one real cluster build.

## Assumptions

These must hold before the cluster AC can pass; none is created by this spec.

- The `build` namespace and the `buildkit-push` dockerconfigjson secret exist in the target cluster, **and that credential covers the registry the arm pushes to**. It authenticates against Docker Hub, which is why the arm pushes `docker.io/bborbe/sentry-alert-consumer`; a private-mirror push fails `401 Unauthorized` after a successful build.
- The repo is public, so the Job's anonymous `git fetch` succeeds; the init container carries no clone credential.
- `crane` is installed on the host running the ACs.
- `REF` exists in the local repository **and on `origin`**: the runner derives `SOURCE_DATE_EPOCH` from the local repository, and the Job's init container fetches the ref from the remote. A commit that exists only locally fails at the clone.
- The build pod can reach the Go module proxy.
- `kubectl` is configured for the target cluster through the alias files `hack/cluster-alias.sh` resolves.
- The prompt container can reach `github.com`, so the two copy-source repositories clone. If it cannot, fall back to copying the five files from a local sibling checkout (`~/Documents/workspaces/agent-task-executor/hack/`, `~/Documents/workspaces/go-skeleton/hack/`).
- The draft PR carrying this approved spec is what triggers the watcher (`dark-factory-implement`, polled every 5 min), and the agent pushes its commits to that PR's branch. `master` is protected, so nothing lands there directly.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| `BUILD_TARGET` names an unknown cluster or context | The runner fails before applying anything and lists the accepted values | Re-run with a listed target; no Job exists to clean up |
| `VERSION` omitted on the cluster arm | Refusal before the Job is created, naming `VERSION` | Re-run passing `VERSION=<scratch-tag>` |
| `REF` is a short SHA, or is not in the local repository | The runner dies resolving `SOURCE_DATE_EPOCH` | `git fetch`, then re-run with the full 40-character SHA |
| `REF` exists locally but was never pushed | The clone init container fails: the ref does not exist on `origin` | Push the branch or commit, then re-run |
| The clone's commit epoch differs from the locally derived one | The build container refuses before `buildctl` runs and names both epochs | `git fetch`, then re-run |
| A Job with the derived name already exists in `build` | The runner refuses and names the existing Job rather than overwriting it | Delete the finished Job, then re-run |
| The runner is interrupted after `apply` | The Job keeps running in the cluster while the local invocation exits; the next run then hits the existing-Job refusal | Inspect `kubectlsun -n build get jobs`, read the pod's log, delete the Job once finished |
| The build pod cannot reach the module proxy | The Job fails; the runner prints the last 100 log lines and the Job name | Inspect the Job logs; the local arm is unaffected |
| The build pod is OOM-killed or throttled | The Job reports `failed`; the pod status carries `OOMKilled` or a CPU-throttling event | Raise the template's limits in the same change, or build locally for that one image |
| The registry throttles or rejects the push | The build fails at the push step with the registry's own error | Re-check the `build` namespace secret, then re-run |
| A caller-supplied `VERSION` names an already-released tag | No guard — the push replaces that tag irreversibly | Re-push the image from the released tag, then cut a new release |
| The built tag is not the one nuke's `MIRROR_IMAGES` references | The push lands in Docker Hub and the mirror copies the stale `CONSUMER_TAG` (`v0.1.0`), so the deployment does not move to the new image | Bump `CONSUMER_TAG` in `bborbe/nuke`'s `sentry-alert-consumer/Makefile` — a separate deploy-side change, out of this spec's scope |
| The local arm regresses | `make build` fails or tags differently | Revert the arm; the local path is the regression gate |

## Security / Abuse Cases

- The runner substitutes caller-supplied values into a manifest and into embedded shell, so every value is checked against a character whitelist and rejected for embedded newlines before rendering. Do not add a value path that bypasses that check.
- The repository URL and the push-secret name are literals in the dispatch, not configurable inputs. The whitelist constrains the *shape* of a URL, not *which* repository is cloned and built with a mounted push credential; a configurable clone source on this build would let a caller point the credential-bearing Job at another repo.
- The build Job mounts a push credential from a `build`-namespace secret and runs with `automountServiceAccountToken: false`. The credential is scoped to pushing an image; it grants nothing else.
- The build container runs as an unprivileged user with the sandbox profiles relaxed because rootless BuildKit requires it. That relaxation is deliberate and must not be copied to the clone init container.
- `hack/build-cluster-bootstrap.sh` reads a TeamVault credential into a `600` temp file and removes it on every exit path. It is never echoed, never logged, and never written into the repo. Do not add a logging path that prints the rendered secret.
- `VERSION` is caller-controlled and becomes an image tag and a Job name. The tag is whitelisted and the Job name is sanitized to a valid DNS name, truncated, and stripped of trailing separators.

## Suggested Decomposition

Prompts should be generated in this order — each row is a single prompt with a clear scope.

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 0 | **Not a prompt.** `.dark-factory.yaml` → `workflow: direct` + `pr: false` (the measured-working shape), keeping `autoRelease: false`, `autoGeneratePrompts: true` and `validationPrompt: docs/dod.md`, plus `defaultBranch: master` and `autoMerge: false`. Pre-landed and **committed** on the branch before approval. Satisfies AC 8's `.dark-factory.yaml` clause, so no prompt re-implements it. | 7 | 8 (config clause only) | — |
| 1 | The `Makefile.docker` dispatch: the `BUILD_TARGET` split, `IMAGE`/`TAG`, the explicitness capture before the default, the composed-reference echo, and local-only `upload`/`clean`/`buca` | 1, 2, 3, 4, 5 | 1, 3, 4, 6 | row 0 |
| 2 | The build path itself: the five `hack/` files and the module-mode image build | 2, 6 | 2, 5, 7 | prompt 1 (the dispatch calls the runner) |
| 3 | Docs: the README build section and the CHANGELOG bullet | 8 | 8 | — |

Rationale: row 0 is pre-landed rather than prompted because the lane config must be correct **before** the agent is asked to run — a prompt that fixes the config it is itself running under is a chicken-and-egg. With it in place, prompt 1 establishes the dispatch, prompt 2 supplies the runner it calls and the Dockerfile change that lets it work from a clone, and prompt 3 is independent and touches no build logic.

## Do-Nothing Option

Leaving this repo without a cluster arm keeps one nukeprod watcher's image tied to the Mac. Every rebuild, every release cut while the operator is away from this machine, and every future unattended pipeline needs a human at the keyboard for this one repo — while its siblings build themselves. The cost is not the one build; it is that the automation has a hole shaped exactly like the repo nobody is watching.
