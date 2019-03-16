# Istio Test Framework

This document introduces the Istio test framework.

For an overview of the architecture as well as how to extend the framework, see the [Developer Guide](DEVELOPER_GUIDE.md).

## Introduction

Writing tests for cloud-based microservices is hard. Getting the tests to run quickly and reliably is difficult in and of itself. However, supporting multiple cloud platforms is another thing altogether. 

The Istio test framework attempts to address these problems. Some of the objectives for the framework are:

- **Writing Tests**:
  - **Platform Agnostic**: The API abstracts away the details of the underlying platform. This allows the developer to focus on testing the Istio logic rather than plumbing the infrastructure.
  - **Reuseable Tests**: Suites of tests can be written which will run against any platform that supports Istio. This effectively makes conformance testing built-in. 
- **Running Tests**:
  - **Standard Tools**: Built on [Go](https://golang.org/)'s testing infrastructure and run with standard commands (e.g. `go test`) .
  - **Easy**: Few or no flags are required to run tests out of the box. While many flags are available, they are provided reasonable defaults where possible.
  - **Fast**: With the ability to run processes natively on the host machine, running tests are orders of magnitude faster.
  - **Reliable**: Running tests natively are inherently more reliable than in-cluster. However, the components for each platform are written to be reliable (e.g. retries).
   
## Getting Started

To begin using the test framework, you'll need a write a `TestMain` that simply calls `framework.Run`:

```golang
func TestMain(m *testing.M) { 
    framework.Run("my_test", m)
}
```

The first parameter is a `TestID`, which can be any string. It's used mainly for creating a working directory for the test output.

The call to `framework.Run` does the following:

1. Starts the platform-specific environment. By default, the native environment is used. To run on Kubernetes, set the flag: `--istio.test.env=kubernetes`.
2. Run all tests in the current package. This is the standard Go behavior for `TestMain`.
3. Stops the environment.

Then, in the same package as `TestMain`, define your tests:

```golang
func TestHTTP(t *testing.T) {
    // Get the test context from the framework.
    ctx := framework.GetContext(t)
    
    // Indicate the components required by this test.
    ctx.RequireOrSkip(t, lifecycle.Test, &ids.Apps)

    // Get the component(s) that you need.
    apps := components.GetApps(ctx, t)
    a := apps.GetAppOrFail("a", t)
    b := apps.GetAppOrFail("b", t)

    // Interact with the components...
    
    be := b.EndpointsForProtocol(model.ProtocolHTTP)[0]
    result := a.CallOrFail(be, components.AppCallOptions{}, t)[0]

    if !result.IsOK() {
        t.Fatalf("HTTP Request unsuccessful: %s", result.Body)
    }
}
```

Every test will follow the pattern in the example above:

1. Get the context. The context is the main API for the test framework.
2. Indicate the requirements for your test. The context will immediately attempt to start the required components. If for some reason, a requirement cannot be met, you can choose whether the test should be skipped (`RequireOrSkip`) or failed (`RequireOrFail`).
3. Get and use components. Each component (e.g. Pilot, Mixer, Apps) defines its own API. See the interface documentation for details on usage.

### Component Lifecycles

When requiring components, you must provide a lifecycle scope. Components in the test framework can be assigned the following lifecycle scopes:

| Scope | Description |
| --- | --- |
| Suite | Used for a component that should exist for duration of the test suite (i.e. until the environment is destroyed). |
| Test | Used for a component that should exist for the duration of a single test method. |
| System | Same as `Suite`, but reserved for Istio system components. Regardless of the scope passed to `RequireXXX`, Istio system components are upgraded automatically to `System`.|

### Supported Platforms

#### Native

Running on the native host platform (i.e. in-process or local processes) is the default. Running natively has several advantages over in-cluster in that they're generally simpler to run, faster, and more reliable.

Running natively requires no flags, since `--istio.test.env=native` is the default. However, at the time of this writing, not all components have been implemented natively.

#### Kubernetes

To run on Kubernetes, specify the flag `--istio.test.env=kubernetes`.  By default, Istio will be deployed using the configuration in `~/.kube/config`.

Several flags are available to customize the behavior, such as:

| Flag | Default | Description |
| --- | --- | --- |
| istio.test.kube.config | `~/.kube/config` | Location of the kube config file to be used. |
| istio.test.kube.systemNamespace | `istio-system` | Namespace for components in the `System` scope. If '', the namespace is generated with the prefix "istio-system-". |
| istio.test.kube.suiteNamespace | `''` | Namespace for components in the `Suite` scope. If '', the namespace is generated with the prefix "istio-suite-". |
| istio.test.kube.testNamespace | `''` | Namespace for components in the `Test` scope. If '', the namespace is generated with the prefix "istio-test-". |
| istio.test.kube.deploy | `true` | If `true`, the components should be deployed to the cluster. Otherwise, it is assumed that the components have already deployed. |
| istio.test.kube.minikubeingress | `false` | If `true` access to the ingress will be via nodeport. Should be set to `true` if running on Minikube. |
| istio.test.kube.helm.chartDir | `$(ISTIO)/install/kubernetes/helm/istio` | |
| istio.test.kube.helm.valuesFile | `values-istio-mcp.yaml` | The name of a file (relative to `istio.test.kube.helm.chartDir`) to provide Helm values. |
| istio.test.kube.helm.values | `''` | A comma-separated list of helm values that will override those provided by `istio.test.kube.helm.valuesFile`. These are overlaid on top of a map containing the following: `global.hub=${HUB}`, `global.tag=${TAG}`, `global.proxy.enableCoreDump=true`, `global.mtls.enabled=true`,`galley.enabled=true`. |

