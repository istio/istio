# AGENTS.md

Working instructions for AI coding agents in the [istio/istio](https://github.com/istio/istio) repository.
This file covers *how to work here*: commands, conventions, and the checks a change must pass.

## Orient Yourself First

Do not start by crawling the directory tree. Read these instead:

| Read this | For |
|-----------|-----|
| [ARCHITECTURE.md](ARCHITECTURE.md) | High-level overview: components, directory layout, data flow, glossary. **Start here.** |
| [.github/.copilot/domain_knowledge/](.github/.copilot/domain_knowledge/) | Deep dives on individual subsystems (see table below). |
| [architecture/](architecture/) | Detailed design docs per subsystem, linked from ARCHITECTURE.md. |

Subsystem notes in `.github/.copilot/domain_knowledge/`:

| File | Covers |
|------|--------|
| `krt_package.md` | `krt`, the declarative controller framework used for most new controllers |
| `pilot_push_context.md` | How `PushContext` is built and turned into xDS messages |
| `integration_test_framework.md` | `pkg/test/framework` — writing and running integration tests |
| `istioctl_commands.md` | `istioctl` command structure and config sources |
| `istio_analysis_messages.md` | Analyzers and analysis messages; how to add one |
| `istio_tags_and_revisions.md` | Tags and revisions in orchestration and xDS generation |

Read the relevant file *before* editing code in that area — these subsystems have
non-obvious invariants that are not apparent from the code alone.

## Build

Builds run **inside the build container by default** (`BUILD_WITH_CONTAINER ?= 1`, set in
`Makefile.overrides.mk`), so `make` plus a container runtime is the only requirement.

```bash
make build                   # Compile all Go binaries
make docker                  # Build container images
make docker.push             # Push images (set HUB= and TAG=)
BUILD_WITH_CONTAINER=0 make  # Use the local Go toolchain instead of the container
```

Go version is pinned in `go.mod` (currently 1.26.0). Common variables: `HUB` (image
registry), `TAG` (image tag).

## Test

```bash
make test                       # All unit tests (alias for racetest: -race -tags=assert)
make test PKG=./pilot/...       # Scope unit tests to a package tree
make test.integration.kube      # All integration tests (needs a cluster)
make test.integration.pilot.kube   # One integration suite
make benchtest                  # Benchmarks
```

- **Unit tests** live in `*_test.go` next to the code. `make test` is an alias for
  `racetest`, so race detection is always on — do not assume a separate step.
- Scope with `PKG=`, not by editing the Makefile.
- **Integration tests** live in `tests/integration/` and must be tagged `//go:build integ`,
  otherwise they silently run as unit tests (`make check-go-tag` enforces this).
- The target pattern is `test.integration.<pkg.path>.kube`, where dots become slashes —
  `tests/integration/helm/` is `test.integration.helm.kube`. The `.kube` suffix comes last.
- Integration tests need a Kubernetes cluster. Use `prow/integ-suite-kind.sh` for a local
  KinD run, or point `INTEGRATION_TEST_KUBECONFIG` / `KUBECONFIG` at an existing cluster.
- Add tests for every behavioral change. Prefer a unit test; reach for an integration test
  only when the behavior genuinely spans components.

## Lint, Format, and Generated Code

```bash
make lint        # All linters (Go, YAML, markdown, scripts, licenses, Helm)
make format      # Auto-format (Go, Python, go mod tidy)
make precommit   # format + lint — run before opening a PR
make gen         # Regenerate protos, CRDs, golden files, licenses
make gen-check   # Verify generated output is committed and the tree is clean
```

Run `make precommit` before proposing a change as finished. If you touched protos, CRDs,
Helm charts, or anything with golden files, run `make gen` and commit the result — CI runs
`gen-check` and fails on an uncommitted diff.

## Repository Gotchas

- **Never hand-edit generated files.** Regenerate with `make gen`. This includes CRD YAML,
  protobuf output, golden test files, and license manifests.
- **Never hand-edit `common/`.** It is vendored from `istio/common-files` and is overwritten
  by `make update-common`.
- **API types live in [istio/api](https://github.com/istio/api)**, not here. Changing a
  `.proto`-backed type requires a PR there first, then a dependency bump here.
- **`manifests/charts/gateways/istio-egress/` is generated** from the ingress chart by
  `make copy-templates`. Edit the ingress chart.
- **Ztunnel is a separate repo** ([istio/ztunnel](https://github.com/istio/ztunnel), Rust).
  Only its Helm chart and workload API live here.
- Prefer `krt` for new controllers over hand-rolled informer logic — see `krt_package.md`.

## Code Conventions

Follow [Effective Go](https://golang.org/doc/effective_go.html) and
[Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments). Repository
specifics, including logging and performance guidance, are in
[.github/copilot-instructions.md](.github/copilot-instructions.md). The essentials:

- Package names lowercase, matching their directory. Types are CamelCase — singular for
  types, plural for collections.
- Command-line flags use dashes, never underscores.
- Log via `istio.io/istio/pkg/log`; prefer structured logging with `WithLabels()`. Errors
  and warnings must be actionable.
- Istiod is on a hot path: minimize allocations, avoid spawning goroutines per request, and
  measure before and after any optimization.
- Match the conventions of the surrounding package over any general rule stated here.

## Commits and Pull Requests

- **Every commit must be signed off** for DCO: `git commit -s`. PRs fail CI without it.
- Keep PRs focused and organized into logical commits, with a description explaining the
  rationale and linking related issues.
- **User-facing changes require a release note** in `releasenotes/notes/<pr-or-issue>.yaml`,
  following `releasenotes/template.yaml`. Set `kind` (bug-fix, security-fix, feature, test)
  and `area` (traffic-management, security, telemetry, installation, istioctl,
  documentation). CI enforces this.
- Communicate intent before large changes — open an issue or a WIP PR first.
- Do not modify a PR's CI configuration to make a failing test pass.

## Scope Discipline for Agents

- Make the change that was asked for. Do not opportunistically reformat, rename, or
  "clean up" unrelated code — it makes review harder and is routinely rejected here.
- If a fix requires a change in `istio/api` or `istio/ztunnel`, say so rather than working
  around it locally.
- Report honestly which checks you actually ran. `make test` and integration tests are slow;
  if you skipped them, say which and why rather than implying they passed.
