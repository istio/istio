# GitHub Copilot Custom Instructions for Istio

This file provides repository-specific instructions for GitHub Copilot, summarizing key conventions, practices, and context from the Istio project. These guidelines are distilled from all README.md files and important documentation in the .wiki directory.

---

## Project Overview
- **Istio** is an open source service mesh that provides a uniform way to secure, connect, and monitor microservices. It is designed to work with Kubernetes and other cluster management platforms.
- The main components are **Envoy** (sidecar proxy), **Istiod** (control plane), and the **Operator** (CLI tool for installation and management).
- The codebase is primarily in Go, with supporting scripts and configuration in Bash, YAML, and other languages.

## Coding Conventions
- Follow [Effective Go](https://golang.org/doc/effective_go.html) and [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments).
- Use clear, concise comments. Follow [Go's commenting conventions](https://blog.golang.org/godoc-documenting-go-code).
- Package names must be lowercase and match their directory. Types use CamelCase. Use singular nouns for types, plural for collections.
- Command-line flags use dashes, not underscores.
- Adhere to the repository's enforced lint rules for all supported languages. Go linting is run via `common/scripts/lint_go.sh` and orchestrated through `common/Makefile.common.mk` targets; see these files for details on linting entrypoints and configuration.

## Logging
- Use the [Istio logging package](https://godoc.org/istio.io/pkg/log) for all logs.
- Log levels: Error, Warn, Info, Debug. Errors and warnings should be actionable and clear.
- Prefer structured logging with `WithLabels()`.

## Testing
- Add tests for all new features and bug fixes. Use unit, integration, or end-to-end tests as appropriate.
- Use `make test` and related targets to run tests. Coverage and race detection are encouraged.

## Performance
- Minimize memory allocations and garbage collection pressure. Prefer value types over pointers when possible.
- Reuse objects and use `sync.Pool` for frequently allocated types.
- Avoid creating goroutines in the main request path; pre-create and reuse them.
- Always measure performance before and after optimizations.

## Pull Requests
- Communicate intent before large changes (open an issue or WIP PR).
- PRs should be short, focused, and well-explained. Organize into logical commits.
- Add a clear description and rationale. List all related issues.
- Add tests for all changes.
- Track follow-up work with issues as needed.

## Build & Development
- Use `make build` to build all components. Use `make docker` to build container images.
- Set environment variables: `HUB`, `TAG`, and `ISTIO` for Docker and build processes.
- Use `make docker.push` to push images to your registry.
- For development, see `.wiki/Preparing-for-Development.md` and `.wiki/Using-the-Code-Base.md`.

## Release Notes
- Add release notes for all user-facing changes in `releasenotes/notes/` using the provided template.
- Release notes should be clear, actionable, and categorized (bug-fix, feature, security, etc.).

## CNI and Operator
- The CNI plugin manages pod networking for Istio, supporting both sidecar and ambient modes. It requires privileged permissions.
- The Operator is now a CLI tool for installing and managing Istio via the IstioOperator API. Use profiles for configuration.

## Related Repositories
- Other repositories in the istio organization are relevant to this repository
- istio/api contains API definitions used by Istio components.
- istio/istio is the main repository for Istio, which includes the control plane and data plane components.
- istio/istio.io contains documentation and website content for Istio.
- istio/test-infra contains testing infrastructure and scripts for Istio.
- istio/ztunnel contains the ztunnel proxy, a secure l4 proxy for Istio Ambient mode.

## Additional Resources
- [Project Wiki](https://github.com/istio/istio/wiki) contains detailed guides on development, conventions, and troubleshooting.
- See `.wiki/Development-Conventions.md`, `.wiki/Writing-Fast-and-Lean-Code.md`, `.wiki/Writing-Good-Pull-Requests.md` for more details.

---

**Copilot should follow these conventions and best practices when generating code, comments, or documentation for this repository.**
