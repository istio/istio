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

## Ambient Waypoint Testing Patterns

### Egress Waypoint with ServiceEntry

For testing waypoint features that involve external services (egress), follow the pattern in `TestWaypointAsEgressGateway` (tests/integration/ambient/waypoint_test.go):

1. **Separate egress namespace**: Claim a non-injected namespace for the waypoint Gateway:
   ```go
   egressNamespace, err := namespace.Claim(t, namespace.Config{
       Prefix: "egress",
       Inject: false,
   })
   ```

2. **Cross-namespace waypoint**: The Gateway uses `allowedRoutes.namespaces.from: Selector` with a label matching the app namespace. ServiceEntry labels reference the waypoint across namespaces:
   ```yaml
   labels:
     istio.io/use-waypoint: <gateway-name>
     istio.io/use-waypoint-namespace: <egress-namespace>
   ```

3. **External backend**: Use `apps.ExternalNamespace` for DNS-resolved endpoints:
   ```yaml
   endpoints:
   - address: external.{{.ExternalNamespace}}.svc.cluster.local
   ```

### Verifying Envoy Cluster Configuration via istioctl

Use `istioctl proxy-config cluster` with JSON output to verify Envoy cluster properties:

```go
waypointLabel := label.IoK8sNetworkingGatewayGatewayName.Name + "=<gateway-name>"
fetchFn := kubetest.NewSinglePodFetch(t.Clusters().Default(), namespace, waypointLabel)
pods, err := kubetest.WaitUntilPodsAreReady(fetchFn)
waypointPod := fmt.Sprintf("%s.%s", pods[0].Name, namespace)

output, _ := istioctl.NewOrFail(t, istioctl.Config{}).InvokeOrFail(t,
    []string{"proxy-config", "cluster", waypointPod, "-o", "json"})

var clusters []json.RawMessage
json.Unmarshal([]byte(output), &clusters)
// Parse each cluster as map[string]any and check fields
```

**Important**: Envoy JSON uses **snake_case** field names (e.g., `dns_lookup_family`, not `dnsLookupFamily`). Cluster type uses string values like `"LOGICAL_DNS"`, `"STRICT_DNS"`, `"STATIC"`.

### Verifying ServiceEntry Status Conditions

Use `IstioCondition` when testing Istio-native ServiceEntry status rather than Kubernetes-native objects like Services/Gateways.

**Constraints:**
* **API:** Use `istio.io/api/meta/v1alpha1.IstioCondition`.
* **Comparison:** Do NOT use `metav1.Condition` (types are incompatible).
* **Field Mapping:** `IstioCondition.Status` is a `string` ("True"/"False"), whereas `metav1.Condition.Status` is an `enum`.
* **Helper:** Use `pilot/pkg/model/status.GetCondition`.

```go
se, err := t.Clusters().Default().Istio().NetworkingV1().ServiceEntries(ns).
    Get(context.TODO(), "name", metav1.GetOptions{})
for _, cond := range se.Status.Conditions {
    // cond is *v1alpha1.IstioCondition
    // cond.Type is string, cond.Status is "True" or "False"
}
```

For helpers: `pilot/pkg/model/status.GetCondition(conditions, type)` works with `[]*v1alpha1.IstioCondition`.
The existing `GetCondition` helper in waypoint_test.go only works with `[]metav1.Condition` (for K8s Services).

### Condition Model

Status conditions are defined in `pilot/pkg/model/service.go`:
- `ConditionType` is a string alias (e.g., `"istio.io/ConnectStrategyWithoutWaypoint"`)
- `model.Condition` has `Status bool` which maps to `"True"`/`"False"` in the IstioCondition
- `GetConditions()` on `ServiceInfo` returns a `ConditionSet` (map of ConditionType to *Condition)
- The status queue in `ambient/statusqueue/conversion.go` converts these to `v1alpha1.IstioCondition` for server-side apply

### Traffic Verification

Final verification of traffic flow in Ambient mesh

**Constraints:**

- **L7 Validation**: Always use `check.And(check.OK(), IsL7())` to verify traffic flows through the waypoint, L7 processing indicates waypoint is involved.
- **Source Filtering**: Use `hboneClient()` helper to filter for HBONE capable sources.
- **Sidecar Limitation**: Skip sidecar sources using src.Config().HasSidecar(); they do not yet fully support cross-namespace use-waypoint logic.

## Related Components

- [istioctl_commands.md](istioctl_commands.md): CLI for interacting with Istio, often used in tests.
- [krt_package.md](krt_package.md): Declarative controller runtime, sometimes tested via the framework.
- [pilot_push_context.md](pilot_push_context.md): Core data structure for configuration, often validated in integration tests.

---

This file should be updated as the test framework evolves and new patterns or utilities are introduced.

## Best Practices

- Use labels to categorize and filter tests (e.g., `label.CustomSetup`, `label.Flaky`).
- Always clean up resources using `defer` or test context cleanup hooks.
- Use the provided resource and environment abstractions instead of direct Kubernetes API calls.
- Prefer `NewTest` and `NewSuite` for test and suite setup to ensure consistent lifecycle management.
- Leverage the logging and telemetry integration for debugging and observability.
