---
status: completed
spec: [001-cluster-build-arm]
summary: 'Added a BUILD_TARGET selector to Makefile.docker: the default local arm keeps the unchanged docker build/tag, the cluster arm prints the composed image reference and dispatches hack/build-cluster.sh, with parse-time guards for an explicit VERSION and local-only targets; added a CHANGELOG Unreleased entry.'
execution_id: 03747306-83e4-562c-aa21-2038ffefaab8-exec-001-makefile-docker-dispatch
dark-factory-version: dev
created: "2026-11-09T20:07:41Z"
queued: "2026-11-09T20:26:49Z"
started: "2026-11-09T20:26:51Z"
completed: "2026-11-09T20:33:30Z"
branch: dark-factory/cluster-build-arm
---

<summary>
- Adds a build-target switch so the image can be built locally (default) or inside a Kubernetes cluster.
- The local build command and the image tag it produces stay exactly as they are today.
- The cluster arm prints the fully composed image reference before it hands off to the cluster runner.
- The cluster arm refuses to run without a caller-supplied tag, so a released tag can never be republished by accident.
- The upload, clean, and publish-all targets stay local-only and are rejected immediately when a cluster target is requested.
- Repository and tag are composed once, so both arms publish to the same image repository and cannot drift.
- No new build knobs are added beyond build target, image, tag, and version.
- The refusal messages name the offending variable or targets before any cluster Job is created.
</summary>

<objective>
Change the Docker build makefile so `BUILD_TARGET` selects between the unchanged local arm (`local`, the default) and a cluster arm that dispatches a named commit to the copied cluster runner. Capture `VERSION`'s origin before the existing parse-time default so the cluster arm can refuse a defaulted tag, and reject `upload`/`clean`/`buca` at parse time when a cluster target is set.
</objective>

<context>
Read these files before editing:
- `Makefile` — note that line 3 includes `Makefile.docker` before line 5 assigns `SERVICE`.
- `Makefile.docker` — the file you are editing.
- `Makefile.precommit` — the `make precommit` gate.
- `Makefile.variables` — `ROOTDIR`.
- `docs/dod.md` — the project definition of done.

Reference documentation (in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-makefile-commands.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/git-workflow.md`

The cluster runner `hack/build-cluster.sh` does not exist yet; it is created by the next prompt. This prompt's verification uses `make -n`, which renders the invocation without executing the runner, so that is expected.
</context>

<requirements>
1. In `Makefile.docker`, add the build-target selector at column 0, before the existing `VERSION` default block:

   ```make
   BUILD_TARGET ?= local
   ```

   The assignment must start in column 0 so `grep -c '^BUILD_TARGET' Makefile.docker` sees it.

2. Immediately before the existing `ifeq ($(VERSION),)` block, capture the variable's origin in a simple-expanded variable:

   ```make
   VERSION_ORIGIN := $(origin VERSION)
   ```

   Do not move or modify the existing default block itself:

   ```make
   ifeq ($(VERSION),)
   	VERSION := $(shell git describe --tags `git rev-list --tags --max-count=1`)
   endif
   ```

   The capture must run before that block. After it, `$(origin VERSION)` is `file` on every invocation and a guard written below it never fires.

3. After the `VERSION` default block, define the composed repository and tag with recursive assignment:

   ```make
   IMAGE = $(DOCKER_REGISTRY)/bborbe/$(SERVICE)
   TAG = $(VERSION)
   ```

   Both must use `=` (recursively expanded), never `:=`. `$(SERVICE)` must appear only here and in recipes: `Makefile` includes `Makefile.docker` before it assigns `SERVICE`, so any `:=` or `ifeq` on `$(SERVICE)` evaluates empty.

4. Add parse-time guards for the non-local branch, before the `build` target is defined:

   ```make
   ifneq ($(BUILD_TARGET),local)
   ifeq ($(filter command line environment,$(VERSION_ORIGIN)),)
   $(error the cluster arm needs an explicit VERSION; pass VERSION=<tag> on the command line or in the environment)
   endif
   ifneq ($(filter upload clean buca,$(MAKECMDGOALS)),)
   $(error upload, clean and buca are local-only; run them with BUILD_TARGET=local)
   endif
   endif
   ```

   These are parse-time `$(error …)` directives, not recipe-level `exit 1`: a recipe-level refusal is invisible to `make -n`, so the acceptance checks could not observe it. The first guard names `VERSION`; the second names `upload`, `clean`, and `buca`.

5. Wrap the `build` target in `ifeq ($(BUILD_TARGET),local)` / `else` / `endif`.

   Local branch — keep the existing prerequisite and recipe, changing only the image argument from `$(DOCKER_REGISTRY)/bborbe/$(SERVICE):$(VERSION)` to `$(IMAGE):$(TAG)` (the resolved value is byte-identical; the tag and command are behaviorally unchanged):

   ```make
   ifeq ($(BUILD_TARGET),local)
   .PHONY: build
   build: check-go-mod
   	@if [ -z "$(VERSION)" ]; then echo "ERROR: no VERSION set and no git tag found — set VERSION=vX.Y.Z or create a tag" >&2; exit 1; fi
   	DOCKER_BUILDKIT=1 \
   	docker build \
   	--pull \
   	--no-cache \
   	--rm=true \
   	--platform=linux/amd64 \
   	--build-arg DOCKER_REGISTRY=$(DOCKER_REGISTRY) \
   	--build-arg BUILD_GIT_COMMIT=$$(git rev-parse --short HEAD) \
   	--build-arg BUILD_DATE=$$(date -u +%Y-%m-%dT%H:%M:%SZ) \
   	-t $(IMAGE):$(TAG) \
   	-f Dockerfile .
   else
   ```

   Cluster branch — no prerequisites (the cluster builds a named ref, not the local `HEAD`, so `check-go-mod` must not run). Print the composed reference first, then dispatch the runner with the repository URL and push-secret name as inline literals:

   ```make
   .PHONY: build
   build:
   	@echo "$(IMAGE):$(TAG)"
   	BUILD_TARGET='$(BUILD_TARGET)' \
   	REPO_URL='https://github.com/bborbe/sentry-alert-consumer.git' \
   	REF='$(REF)' \
   	TAG='$(TAG)' \
   	DOCKERFILE='Dockerfile' \
   	IMAGE='$(IMAGE)' \
   	DOCKER_REGISTRY='$(DOCKER_REGISTRY)' \
   	PUSH='true' \
   	PUSH_SECRET='buildkit-push' \
   	TIMEOUT='3600' \
   	hack/build-cluster.sh
   endif
   ```

   The `hack/build-cluster.sh` invocation line must begin with whitespace followed by `hack/build-cluster.sh` so `grep -cE '^[[:space:]]*hack/build-cluster\.sh' Makefile.docker` matches an invocation, not a comment.

6. Keep the `check-go-mod` target exactly as it is today. It stays reachable only through the local `build` prerequisite.

7. Keep the `upload`, `clean`, `apply`, and `buca` recipes exactly as they are today. Do not add a cluster branch to them: the parse-time `$(error …)` in step 4 is the only refusal.

8. Do not add any of these variables: `ALLOW_DEFAULT_VERSION`, `BUILD_TIMEOUT`, `BUILD_REPO_URL`, `BUILD_DOCKERFILE`, `BUILD_PUSH_SECRET`, `BUILD_NAMESPACE`, `CLUSTER_IMAGE`. The repository URL, Dockerfile path, push-secret name, and timeout are inline literals in the cluster recipe. The only knobs are `BUILD_TARGET`, `IMAGE`, `TAG`, and `VERSION`.

9. Keep `DOCKER_REGISTRY ?= docker.io` at the top of the file. The composed reference must remain `docker.io/bborbe/sentry-alert-consumer` when no override is supplied.

10. Do not modify any other file. Do not create `hack/`, do not touch `Dockerfile`, and do not touch `README.md`, `CHANGELOG.md`, or `.dark-factory.yaml`.
</requirements>

<constraints>
- The local arm's command and its tag must not change. The local `docker build` invocation still produces `docker.io/bborbe/sentry-alert-consumer:$(VERSION)`.
- `VERSION`'s explicitness must be captured before `Makefile.docker`'s existing default assignment. After that `ifeq` block the variable's origin is `file` on every invocation, so a guard written below it never fires.
- `$(SERVICE)` may appear only in recipes or recursively-expanded (`=`) variables. `Makefile:3` includes `Makefile.docker` before `Makefile:5` assigns `SERVICE`, so a `:=` or an `ifeq` on it evaluates empty.
- The local-only refusal for `upload` / `clean` / `buca` is a `$(error …)` keyed on the goal list inside the non-local branch, not a recipe-level `exit 1`. A recipe-level refusal is invisible to `make -n`, so the acceptance checks could not observe it.
- `IMAGE`, `SERVICE`, the registry and the tag must each satisfy the runner's character whitelist; no value may carry a newline. Do not introduce a value path that bypasses the runner's checks.
- The repository URL and the push-secret name are literals in the dispatch, not configurable inputs.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
</constraints>

<verification>
Run these from the repository root; all are container-executable and none executes the cluster runner.

```bash
# The selector assignment exists at column 0.
test "$(grep -c '^BUILD_TARGET' Makefile.docker)" -ge 1

# The cluster branch invokes the runner on a recipe line, not in a comment.
test "$(grep -cE '^[[:space:]]*hack/build-cluster\.sh' Makefile.docker)" -ge 1

# The local arm is unchanged: docker build, no cluster runner.
ROOTDIR="$PWD" make -n build > /tmp/local-build.txt
grep -q 'docker build' /tmp/local-build.txt
if grep -q 'hack/build-cluster.sh' /tmp/local-build.txt; then exit 1; fi

# The cluster arm prints the composed reference and calls the runner.
ROOTDIR="$PWD" make -n build BUILD_TARGET=sun REF=0123456789abcdef0123456789abcdef01234567 VERSION=probe > /tmp/cluster-build.txt
grep -q 'hack/build-cluster.sh' /tmp/cluster-build.txt
grep -q 'docker.io/bborbe/sentry-alert-consumer:probe' /tmp/cluster-build.txt

# The cluster arm refuses a tag it did not receive.
if ROOTDIR="$PWD" make -n build BUILD_TARGET=sun REF=0123456789abcdef0123456789abcdef01234567 > /tmp/no-version.txt 2>&1; then exit 1; fi
grep -q 'VERSION' /tmp/no-version.txt

# The local-only targets are refused at parse time when a cluster target is set.
if ROOTDIR="$PWD" make -n buca BUILD_TARGET=sun REF=0123456789abcdef0123456789abcdef01234567 VERSION=probe > /tmp/local-only.txt 2>&1; then exit 1; fi
grep -q 'local-only' /tmp/local-only.txt

# BUILD_TARGET leaked into no file outside the dispatch, docs, runner, or spec.
test -z "$(grep -rn 'BUILD_TARGET' --exclude-dir=vendor --exclude-dir=prompts --exclude-dir=specs . | grep -vE '(Makefile\.docker|README\.md|CHANGELOG\.md|hack/)')"

ROOTDIR="$PWD" make precommit
```
</verification>
