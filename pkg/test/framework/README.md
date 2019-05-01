# Istio Test Framework

This document introduces the Istio test framework.

For an overview of the architecture as well as how to extend the framework, see the [Developer Guide](https://github.com/istio/istio/wiki/Preparing-for-Development).

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

To begin using the test framework, you'll need a write a `TestMain` that simply calls `framework.NewSuite`:

```golang
func TestMain(m *testing.M) { 
    framework.
        NewSuite("my_test", m).
        Run()
}
```

The first parameter is a `TestID`, which can be any string. It's used mainly for creating a working directory for the test output.

The call to `framework.NewSuite` does the following:

1. Starts the platform-specific environment. By default, the native environment is used. To run on Kubernetes, set the flag: `--istio.test.env=kube`.
2. Run all tests in the current package. This is the standard Go behavior for `TestMain`.
3. Stops the environment.

Then, in the same package as `TestMain`, define your tests:

```golang
func TestHTTP(t *testing.T) {
    // Get the test context from the framework.
    ctx := framework.GetContext(t)
    defer ctx.Done(t)
    
    // Get the component(s) that you need.
    apps := components.GetApps(t, ctx)
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
2. Get and use components. Each component (e.g. Pilot, Mixer, Apps) defines its own API. See the interface documentation for details on usage.

If you need to do suite-level checks, then you can pass additional parameters to `framework.TestSuite` returned from `framework.NewTest`:

```golang
func TestMain(m *testing.M) {
    framework.NewTest("my_test", m).
    RequireEnvironment(environment.Kube).                              // Require Kubernetes environment.
    SetupOnEnv(environment.Kube, istio.Setup(&ist, setupIstioConfig)). // Deploy Istio, to be used by the whole suite.
    Setup(setup). // Call your setup function.
    Run()
}

func setupIstioConfig(cfg *istio.Config) {
    cfg.Values["your-feature-enabled"] = "true"
}

func setup(ctx resource.Context) error {
  // ...
}

```

### Supported Platforms

#### Native

Running on the native host platform (i.e. in-process or local processes) is the default. Running natively has several advantages over in-cluster in that they're generally simpler to run, faster, and more reliable.

Running natively requires no flags, since `--istio.test.env=native` is the default. However, at the time of this writing, not all components have been implemented natively.

#### Kubernetes

To run on Kubernetes, specify the flag `--istio.test.env=kube`.  By default, Istio will be deployed using the configuration in `~/.kube/config`.

Several flags are available to customize the behavior, such as:

| Flag | Default | Description |
| --- | --- | --- |
| istio.test.env | `native` | Specify the environment to run the tests against. Allowed values are: `kube`, `native`. Defaults to `native`. |
| istio.test.work_dir | '' | Local working directory for creating logs/temp files. If left empty, os.TempDir() is used. |
| istio.test.hub | '' | Container registry hub to use. If not specified, `HUB` environment value will be used. |
| istio.test.tag | '' | Common container tag to use when deploying container images. If not specified `TAG` environment value will be used. |
| istio.test.pullpolicy | `Always` | Common image pull policy to use when deploying container images. If not specified `PULL_POLICY` environment value will be used. Defaults to `Always` |
| istio.test.nocleanup | `false` | Do not cleanup resources after test completion. |
| istio.test.ci | `false` | Enable CI Mode. Additional logging and state dumping will be enabled. |
| istio.test.kube.config | `~/.kube/config` | Location of the kube config file to be used. |
| istio.test.kube.minikube | `false` | If `true` access to the ingress will be via nodeport. Should be set to `true` if running on Minikube. |
| istio.test.kube.systemNamespace | `istio-system` | Namespace for Istio deployment. If '', the namespace is generated with the prefix "istio-system-". |
| istio.test.kube.deploy | `true` | If `true`, the components should be deployed to the cluster. Otherwise, it is assumed that the components have already deployed. |
| istio.test.kube.helm.chartDir | `$(ISTIO)/install/kubernetes/helm/istio` | |
| istio.test.kube.helm.valuesFile | `values-e2e.yaml` | The name of a file (relative to `istio.test.kube.helm.chartDir`) to provide Helm values. |
| istio.test.kube.helm.values | `''` | A comma-separated list of helm values that will override those provided by `istio.test.kube.helm.valuesFile`. These are overlaid on top of a map containing the following: `global.hub=${HUB}`, `global.tag=${TAG}`, `global.proxy.enableCoreDump=true`, `global.mtls.enabled=true`,`galley.enabled=true`. |

