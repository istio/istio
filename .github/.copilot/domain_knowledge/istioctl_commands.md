# istioctl

This document provides an overview of `istioctl`, including its command structure and the configuration sources it can read from.

## Introduction

`istioctl` is the primary command-line tool for interacting with Istio. It enables users to install, configure, analyze, and debug Istio resources and the service mesh.

## Conceptual Overview

- `istioctl` provides a rich set of subcommands for installation, validation, analysis, debugging, and traffic management.
- It can operate on both live Kubernetes clusters and local configuration files.
- The tool is extensible, with new commands and analyzers added as Istio evolves.

## Implementation Architecture

1. **Command Structure**: Built using the Cobra library, `istioctl` organizes commands hierarchically (root command, subcommands, flags).
2. **Configuration Sources**: Commands can read from live clusters, local files, stdin, or URLs.
3. **Execution Flow**: Each command parses input, loads configuration, performs the requested operation, and outputs results to stdout or files.

### Key Relationships
- Subcommands are registered in the root command (`istioctl/cmd/root.go`).
- Each subcommand implements its logic in a dedicated package under `istioctl/cmd/`.
- Shared utilities and analyzers are in `istioctl/pkg/`.

## Code Implementation

### Command Structure Example

```go
// From istioctl/cmd/root.go
rootCmd := &cobra.Command{
    Use:   "istioctl",
    Short: "Istio control interface",
}

rootCmd.AddCommand(
    install.NewCommand(),
    analyze.NewCommand(),
    proxyconfig.NewCommand(),
    ... // other subcommands
)
```

### Reading Configuration Sources

- **Kubernetes Cluster**: Most commands default to reading from the current kubeconfig context.
- **YAML Files**: Many commands accept `-f` or `--filename` to read from local files or directories.
- **Stdin**: Use `-f -` to read from standard input.
- **URLs**: Some commands accept URLs as input sources for manifests or configs.

#### Example: Analyzing a Local File

```bash
istioctl analyze -f my-config.yaml
```

#### Example: Analyzing Live Cluster State

```bash
istioctl analyze -A
```

#### Example: Installing from a Profile URL

```bash
istioctl install --set profile=demo --set values.global.hub=myrepo
```

## Key Interfaces/Models

- Cobra command structure: `istioctl/cmd/root.go`, `istioctl/cmd/`
- Analyzer framework: `istioctl/pkg/analyze/`
- Input readers: `istioctl/pkg/util/filename.go`, `istioctl/pkg/util/kubernetes.go`

## Example Use Cases

- Installing Istio with custom configuration
- Validating and analyzing configuration before deployment
- Inspecting and debugging proxy configuration
- Upgrading or uninstalling Istio

## Best Practices

- Use `istioctl analyze` before applying changes to catch issues early.
- Prefer local file analysis in CI/CD for fast feedback.
- Use explicit kubeconfig and context flags for multi-cluster environments.
- Keep `istioctl` up to date with your control plane version.

## Related Components

- [Analysis Messages](istio_analysis_messages.md): Diagnostics produced by `istioctl analyze`
