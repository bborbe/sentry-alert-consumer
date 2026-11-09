---
status: completed
spec: [001-cluster-build-arm]
summary: Copied the five cluster-build helper files byte-for-byte into hack/ and switched the Dockerfile build from -mod=vendor to -mod=readonly, with CHANGELOG updated
execution_id: 03747306-83e4-562c-aa21-2038ffefaab8-exec-002-hack-runner-and-module-build
dark-factory-version: dev
created: "2026-11-09T20:07:41Z"
queued: "2026-11-09T20:31:55Z"
started: "2026-11-09T20:33:31Z"
completed: "2026-11-09T20:34:49Z"
branch: dark-factory/cluster-build-arm
---

<summary>
- Adds the five cluster-build helper files that render and run a Kubernetes Job.
- The runner resolves cluster aliases, validates every substituted value, applies the Job, waits for it, and reports its logs.
- The bootstrap helper provisions the build namespace and the push credential; it is run by hand, not by a build.
- The image build now resolves Go modules instead of requiring a vendor directory.
- A fresh clone with no local preparation can build the image the same way a developer checkout does.
- The local build path is unaffected; it runs the same Dockerfile and simply fetches dependencies differently.
- The copied files keep the source repositories' hardening and credential handling unchanged.
</summary>

<objective>
Create the `hack/` directory with the five cluster-build files copied unmodified from the two public sibling repositories, and switch the Dockerfile from vendor mode to module mode so a Job that clones the repo with no `vendor/` can build the image.
</objective>

<context>
Read these files before editing:
- `Makefile.docker` — the cluster branch added by the previous prompt calls `hack/build-cluster.sh` and passes the runner's environment variables.
- `Dockerfile` — the image build you are changing.
- `docs/dod.md` — the project definition of done.

Reference documentation (in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-dockerfile-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/k8s-manifest-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-k8s-binary-conventions.md`

Copy sources (both public, reachable by `git clone` from the container):
- `https://github.com/bborbe/agent-task-executor.git` — `hack/build-cluster.sh`, `hack/cluster-alias.sh`, `hack/build-job.yaml`.
- `https://github.com/bborbe/go-skeleton.git` — `hack/build-cluster-bootstrap.sh`, `hack/dockerconfig.json.tpl`.

The agent-task-executor Job template is the one to use because it carries `automountServiceAccountToken: false`; the go-skeleton template does not. The runner sources `hack/cluster-alias.sh` and fails at that line if it is missing.
</context>

<requirements>
1. Create the `hack/` directory at the repository root.

2. Copy the five files byte-for-byte from `origin/master` of the two public repositories. Use a shallow clone into a temporary directory:

   ```bash
   tmp="$(mktemp -d)"
   git clone --depth=1 --branch master https://github.com/bborbe/agent-task-executor.git "$tmp/agent-task-executor"
   git clone --depth=1 --branch master https://github.com/bborbe/go-skeleton.git "$tmp/go-skeleton"
   mkdir -p hack
   cp "$tmp/agent-task-executor/hack/build-cluster.sh" hack/build-cluster.sh
   cp "$tmp/agent-task-executor/hack/cluster-alias.sh" hack/cluster-alias.sh
   cp "$tmp/agent-task-executor/hack/build-job.yaml" hack/build-job.yaml
   cp "$tmp/go-skeleton/hack/build-cluster-bootstrap.sh" hack/build-cluster-bootstrap.sh
   cp "$tmp/go-skeleton/hack/dockerconfig.json.tpl" hack/dockerconfig.json.tpl
   chmod +x hack/build-cluster.sh hack/cluster-alias.sh hack/build-cluster-bootstrap.sh
   cmp "$tmp/agent-task-executor/hack/build-cluster.sh" hack/build-cluster.sh
   cmp "$tmp/agent-task-executor/hack/cluster-alias.sh" hack/cluster-alias.sh
   cmp "$tmp/agent-task-executor/hack/build-job.yaml" hack/build-job.yaml
   cmp "$tmp/go-skeleton/hack/build-cluster-bootstrap.sh" hack/build-cluster-bootstrap.sh
   cmp "$tmp/go-skeleton/hack/dockerconfig.json.tpl" hack/dockerconfig.json.tpl
   ```

   The `cmp` calls must exit 0. Do not edit, reformat, or re-comment any of the five files.

3. If `github.com` is unreachable, copy the same five files from a local sibling checkout instead: `~/Documents/workspaces/agent-task-executor/hack/` for the first three and `~/Documents/workspaces/go-skeleton/hack/` for the last two. The primary path is the clone; the fallback exists only for an offline container. Do not hand-write the files.

4. The expected files and their executable bits after the copy:

   | File | Mode |
   |---|---|
   | `hack/build-cluster.sh` | `755` |
   | `hack/cluster-alias.sh` | `755` |
   | `hack/build-cluster-bootstrap.sh` | `755` |
   | `hack/build-job.yaml` | `644` |
   | `hack/dockerconfig.json.tpl` | `644` |

5. In `Dockerfile`, replace the vendor-mode flag in the `go build` line. Change:

   ```dockerfile
   RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -mod=vendor -ldflags "-s" -a -installsuffix cgo -o /main
   ```

   to:

   ```dockerfile
   RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -mod=readonly -ldflags "-s" -a -installsuffix cgo -o /main
   ```

   `-mod=readonly` is Go's default module mode: it downloads modules from the proxy and refuses to rewrite `go.mod`, so a fresh clone with no `vendor/` builds and an incomplete `go.mod` fails loudly. Do not use `-mod=mod`.

6. Do not change any other Dockerfile line. Keep `ARG DOCKER_REGISTRY=docker.io` as the default and keep the `FROM ${DOCKER_REGISTRY}/...` base images. The Job template passes `DOCKER_REGISTRY=docker.io` as a build argument, so the base images resolve from the public registry inside the cluster; do not change that back to the private mirror.

7. Do not add a `go mod vendor` step anywhere. The cluster arm builds a named ref, which need not be the local `HEAD`; host-side vendoring would leave a `vendor/` that `COPY .` picks up and Go would then select vendor mode automatically, reintroducing the local/cluster divergence. The existing `check-go-mod` target stays local-only and is not invoked by the cluster arm.

8. Do not modify `Makefile.docker`, `Makefile`, `README.md`, `CHANGELOG.md`, or `.dark-factory.yaml` in this prompt. The runner expects the environment variables that the previous prompt's dispatch already sets.
</requirements>

<constraints>
- The five `hack/` files are copied unmodified from `origin/master`: `hack/build-cluster.sh`, `hack/cluster-alias.sh` and `hack/build-job.yaml` from `agent-task-executor`, and `hack/build-cluster-bootstrap.sh` plus `hack/dockerconfig.json.tpl` from `go-skeleton`. `cluster-alias.sh` is required — the runner sources it and fails at that line without it.
- The Job template already passes `DOCKER_REGISTRY=docker.io` as a build argument, so the Dockerfile's base images resolve from the public registry inside the cluster rather than from the private mirror. Do not change that back to the mirror.
- The clone init container keeps `allowPrivilegeEscalation: false`, `capabilities.drop: ["ALL"]`, and `seccompProfile: RuntimeDefault`. The build container's `Unconfined` seccomp and AppArmor profiles are deliberate because rootless BuildKit requires them; do not copy that relaxation to the clone init container.
- The build Job mounts a push credential from a `build`-namespace secret and runs with `automountServiceAccountToken: false`. The credential is scoped to pushing an image; it grants nothing else. Do not add a logging path that prints the rendered secret.
- `hack/build-cluster-bootstrap.sh` reads a TeamVault credential into a `600` temp file and removes it on every exit path. It is never echoed, never logged, and never written into the repo. Do not add a logging path that prints the rendered secret.
- The runner substitutes caller-supplied values into a manifest and into embedded shell, so every value is checked against a character whitelist and rejected for embedded newlines before rendering. Do not add a value path that bypasses that check.
- `VERSION` is caller-controlled and becomes an image tag and a Job name. The tag is whitelisted and the Job name is sanitized to a valid DNS name, truncated, and stripped of trailing separators.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
</constraints>

<verification>
Run these from the repository root; all are container-executable.

```bash
# The five files exist.
ls hack/
test -f hack/build-cluster.sh
test -f hack/cluster-alias.sh
test -f hack/build-job.yaml
test -f hack/build-cluster-bootstrap.sh
test -f hack/dockerconfig.json.tpl

# The shell runners parse.
bash -n hack/build-cluster.sh
bash -n hack/cluster-alias.sh
bash -n hack/build-cluster-bootstrap.sh

# The Dockerfile no longer forces vendor mode.
test "$(grep -c '\-mod=vendor' Dockerfile)" -eq 0
grep -q '\-mod=readonly' Dockerfile

ROOTDIR="$PWD" make precommit
ROOTDIR="$PWD" make test
```
</verification>
