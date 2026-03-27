# Istio Integration Test Framework

The Istio integration test framework (`pkg/test/framework`) provides a robust, extensible foundation for writing, running, and managing integration and end-to-end tests for Istio. It orchestrates test environments, manages resources, and provides utilities for common testing patterns across multiple clusters and platforms.

## Overview

The framework enables developers to write expressive, reliable, and maintainable tests for Istio features and behaviors. It abstracts away environment setup, resource lifecycle, and multi-cluster orchestration, allowing test authors to focus on test logic and assertions.

## Architecture

- **Test Suite**: Organizes and runs groups of related tests, handling setup and teardown.
- **Test Context**: Provides per-test context, including access to resources, configuration, and logging.
- **Resource Management**: Handles the lifecycle of test resources (e.g., clusters, namespaces, Istio components).
- **Labeling and Selection**: Supports labeling tests and suites for selective execution.
- **Environment Abstraction**: Supports running tests on different environments (Kubernetes, native, etc.).
- **Logging and Telemetry**: Integrates with Istio's logging and tracing for observability.

## Implementation Details

### Key Patterns

#### Defining and Running a Test Suite

```go
// From pkg/test/framework/suite.go
func TestMain(m *testing.M) {
    framework.NewSuite("my_suite").
        Label(label.CustomSetup).
        Setup(mySetupFunction).
        Run()
}
```

#### Writing a Test

```go
// From pkg/test/framework/test.go
framework.NewTest(t).
    Label(label.CustomSetup).
    Run(func(ctx framework.TestContext) {
        // Test logic here
    })
```

#### Accessing Test Context

```go
// From pkg/test/framework/testcontext.go
func MyTest(ctx framework.TestContext) {
    cluster := ctx.Clusters().Default()
    // Use cluster to deploy resources, run checks, etc.
}
```

#### Resource Management

```go
// From pkg/test/framework/resource.go
ns := namespace.NewOrFail(ctx, namespace.Config{
    Prefix: "test",
    Inject: true,
})
defer ns.Delete()
```

### Component Flow

1. **Suite Initialization**: `TestMain` initializes the suite, sets up environments, and registers setup/teardown hooks.
2. **Test Registration**: Individual tests are registered with labels and requirements.
3. **Environment Setup**: The framework provisions clusters, namespaces, and Istio components as needed.
4. **Test Execution**: Each test runs in isolation, with access to a `TestContext` for resource management and assertions.
5. **Teardown**: Resources are cleaned up and logs are collected.

## Key Files

- `pkg/test/framework/suite.go`: Test suite orchestration and lifecycle.
- `pkg/test/framework/test.go`: Test definition and execution.
- `pkg/test/framework/testcontext.go`: Test context and resource access.
- `pkg/test/framework/runtime.go`: Runtime environment management.
- `pkg/test/framework/resource.go`: Resource lifecycle and utilities.
- `pkg/test/framework/logging.go`: Logging integration.
- `pkg/test/framework/operations.go`: Test context creation and test runner entrypoints.

## Examples

### Example: Simple Integration Test

```go
// From pkg/test/framework/integration/main_test.go
func TestExample(t *testing.T) {
    framework.NewTest(t).
        Run(func(ctx framework.TestContext) {
            // Deploy resources, run checks, etc.
        })
}
```

### Example: Multi-Cluster Test

```go
// From pkg/test/framework/testcontext.go
func TestMultiCluster(ctx framework.TestContext) {
    for _, c := range ctx.Clusters().Primaries() {
        // Deploy and validate on each primary cluster
    }
}
```

## Best Practices

- Use labels to categorize and filter tests (e.g., `label.CustomSetup`, `label.Flaky`).
- Always clean up resources using `defer` or test context cleanup hooks.
- Use the provided resource and environment abstractions instead of direct Kubernetes API calls.
- Prefer `NewTest` and `NewSuite` for test and suite setup to ensure consistent lifecycle management.
- Leverage the logging and telemetry integration for debugging and observability.

## Related Components

- [istioctl_commands.md](istioctl_commands.md): CLI for interacting with Istio, often used in tests.
- [krt_package.md](krt_package.md): Declarative controller runtime, sometimes tested via the framework.
- [pilot_push_context.md](pilot_push_context.md): Core data structure for configuration, often validated in integration tests.

---

This file should be updated as the test framework evolves and new patterns or utilities are introduced.
