# Istio Integration Tests

This folder contains Istio integration tests that use the test framework checked in at
[istio.io/istio/pkg/test/framework](https://github.com/istio/istio/tree/master/pkg/test/framework).

## Table of Contents

1. [Overview](#overview)
1. [Writing Tests](#writing-tests)
    1. [Adding a Test Suite](#adding-a-test-suite)
    1. [Sub-Tests](#sub-tests)
    1. [Parallel Tests](#parallel-tests)
    1. [Using Components](#using-components)
    1. [Writing Components](#writing-components)
1. [Running Tests](#running-tests)
    1. [Test Parallelism and Kubernetes](#test-parellelism-and-kubernetes)
    1. [Test Selection](#test-selection)
    1. [Running Tests on CI](#running-tests-on-ci)
        1. [Step 1: Add a Test Script](#step-1-add-a-test-script)
        1. [Step 2: Add a Prow Job](#step-2-add-a-prow-job)
        1. [Step 3: Update TestGrid](#step-3-update-testgrid)
1. [Environments](#environments)
1. [Diagnosing Failures](#diagnosing-failures)
    1. [Working Directory](#working-directory)
    1. [Enabling CI Mode](#enabling-ci-mode)
    1. [Preserving State (No Cleanup)](#preserving-state-no-cleanup)
    1. [Additional Logging](#additional-logging)
    1. [Running Tests Under Debugger](#running-tests-under-debugger-goland)
1. [Reference](#reference)
    1. [Helm Values Overrides](#helm-values-overrides)
    1. [Commandline Flags](#command-line-flags)
1. [Notes](#notes)
    1. [Running on a Mac](#running-on-a-mac)

## Overview

The goal of the framework is to make it as easy as possible to author and run tests. In its simplest
case, just typing ```go test ./...``` should be sufficient to run tests.

This guide walks through the basics of writing tests with the Istio test framework. For best
practices, see [Writing Good Integration Tests](https://github.com/istio/istio/wiki/Writing-Good-Integration-Tests).

## Writing Tests

The test framework is designed to work with standard go tooling and allows developers
to write environment-agnostics tests in a high-level fashion.

### Adding a Test Suite

All tests that use the framework, must run as part of a *suite*. Only a single suite can be defined per package, since
it is bootstrapped by a Go `TestMain`, which has the same restriction.

To begin, create a new folder for your suite under
[tests/integration](https://github.com/istio/istio/tree/master/tests/integration).

```console
$ cd ${ISTIO}/tests/integration
$ mkdir mysuite
```

Within that package, create a `TestMain` to bootstrap the test suite:

```go
func TestMain(m *testing.M) {
    framework.
        NewSuite("mysuite", m).
        Run()
}
```

Next, define your tests in the same package:

```go
func TestMyLogic(t *testing.T) {
    framework.
        NewTest(t).
        Run(func(ctx framework.TestContext) {
            // Create a component
            p := pilot.NewOrFail(ctx, ctx, cfg)

            // Use the component.
            // Apply Kubernetes Config
            ctx.ApplyConfigOrFail(ctx, nil, mycfg)

            // Do more stuff here.
        }
}
```

The `framework.TestContext` is a wrapper around the underlying `testing.T` and implements the same interface. Test code
should generally not interact with the `testing.T` directly.

In the `TestMain`, you can also restrict the test to particular environment, apply labels, or do test-wide setup, such as
 deploying Istio.

```go
func TestMain(m *testing.M) {
    framework.
        NewSuite("mysuite", m).
        // Deploy Istio on the cluster
        Setup(istio.Setup(nil, nil)).
        // Run your own custom setup
        Setup(mySetup).
        Run()
}

func mySetup(ctx resource.Context) error {
    // Your own setup code
    return nil
}
```

### Sub-Tests

Go allows you to run sub-tests with `t.Run()`. Similarly, this framework supports nesting tests with `ctx.NewSubTest()`:

```go
func TestMyLogic(t *testing.T) {
    framework.
        NewTest(t).
        Run(func(ctx framework.TestContext) {

            // Create a component
            g := galley.NewOrFail(ctx, ctx, cfg)

            configs := []struct{
                name: string
                yaml: string
            } {
                // Some array of YAML
            }

            for _, cfg := range configs {
                ctx.NewSubTest(cfg.name).
                    Run(func(ctx framework.TestContext) {
                        ctx.ApplyConfigOrFail(ctx, nil, mycfg)
                        // Do more stuff here.
                    })
            }
        })
}
```

Under the hood, calling `subtest.Run()` delegates to `t.Run()` in order to create a child `testing.T`.

### Parallel Tests

Many tests can take a while to start up for a variety of reasons, such as waiting for pods to start or waiting
for a particular piece of configuration to propagate throughout the system. Where possible, it may be desirable
to run these sorts of tests in parallel:

```go
func TestMyLogic(t *testing.T) {
    framework.
        NewTest(t).
        RunParallel(func(ctx framework.TestContext) {
            // ...
        }
}
```

Under the hood, this relies on Go's `t.Parallel()` and will, therefore, have the same behavior.

A parallel test will run in parallel with siblings that share the same parent test. The parent test function
will exit before the parallel children are executed. It should be noted that if the parent test is prevented
from exiting (e.g. parent test is waiting for something to occur within the child test), the test will
deadlock.

Consider the following example:

```go
func TestMyLogic(t *testing.T) {
    framework.NewTest(t).
        Run(func(ctx framework.TestContext) {
            ctx.NewSubTest("T1").
                Run(func(ctx framework.TestContext) {
                    ctx.NewSubTest("T1a").
                        RunParallel(func(ctx framework.TestContext) {
                            // Run in parallel with T1b
                        })
                    ctx.NewSubTest("T1b").
                        RunParallel(func(ctx framework.TestContext) {
                            // Run in parallel with T1a
                        })
                    // Exits before T1a and T1b are run.
                })

            ctx.NewSubTest("T2").
                Run(func(ctx framework.TestContext) {
                    ctx.NewSubTest("T2a").
                        RunParallel(func(ctx framework.TestContext) {
                            // Run in parallel with T2b
                        })
                    ctx.NewSubTest("T2b").
                        RunParallel(func(ctx framework.TestContext) {
                            // Run in parallel with T2a
                        })
                    // Exits before T2a and T2b are run.
                })
        })
}
```

In the example above, non-parallel parents T1 and T2 contain parallel children T1a, T1b, T2a, T2b.

Since both T1 and T2 are non-parallel, they are run synchronously: T1 followed by T2. After T1 exits,
T1a and T1b are run asynchronously with each other. After T1a and T1b complete, T2 is then run in the
same way: T2 exits, then T2a and T2b are run asynchronously to completion.

### Using Components

The framework itself is just a platform for running tests and tracking resources. Without these `resources`, there
isn't much added value. Enter: components.

Components are utilities that provide abstractions for Istio resources. They are maintained in the
[components package](https://github.com/istio/istio/tree/master/pkg/test/framework/components), which defines
various Istio components such as galley, pilot, and namespaces.

Each component defines their own API which simplifies their use from test code, abstracting away the
environment-specific details. This means that the test code can (and should, where possible) be written in an
environment-agnostic manner, so that they can be run against any Istio implementation.

For example, the following code creates and then interacts with a Galley and Pilot component:

```go
func TestMyLogic(t *testing.T) {
    framework.
        NewTest(t).
        Run(func(ctx framework.TestContext) {
            // Create the components.
            g := galley.NewOrFail(ctx, ctx, galley.Config{})
            p := pilot.NewOrFail(ctx, ctx, pilot.Config {})

            // Apply configuration via Galley.
            ctx.ApplyConfigOrFail(ctx, nil, mycfg)

            // Wait until Pilot has received the configuration update.
            p.StartDiscoveryOrFail(t, discoveryRequest)
            p.WatchDiscoveryOrFail(t, timeout,
                func(response *xdsapi.DiscoveryResponse) (b bool, e error) {
                    // Validate that the discovery response has the configuration applied.
                })
            // Do more stuff...
        }
}
```

When a component is created, the framework tracks its lifecycle. When the test exits, any components that were
created during the test are automatically closed.

### Writing Components

To add a new component, you'll first need to create a top-level folder for your component under the
[components folder](https://github.com/istio/istio/tree/master/pkg/test/framework/components).

```console
$ cd ${ISTIO}/pkg/test/framework/components
$ mkdir mycomponent
```

You'll then need to define your component's API.

```go
package mycomponent

type Instance interface {
    resource.Resource

    DoStuff() error
    DoStuffOrFail(t test.Failer)
}
```

| NOTE: A common pattern is to provide two versions of many methods: one that returns an error as well as an `OrFail` version that fails the test upon encountering an error. This provides options to the calling test and helps to simplify the calling logic. |
| --- |

Next you need to implement your component for one or more environments. If possible, create both a native and Kubernetes version.

```go
package mycomponent

type nativeComponent struct {
    id resource.ID
    // ...
}

func newNative(ctx resource.Context) (Instance, error) {
    if config.Galley == nil {
        return nil, errors.New("galley must be provided")
    }

    instance := &nativeComponent{}
    instance.id = ctx.TrackResource(instance)

    //...
    return instance, nil
}

func (c *nativeComponent) ID() resource.ID {
    return c.id
}
```

Each implementation of the component must implement `resource.Resource`, which just exposes a unique identifier for your
component instances used for resource tracking by the framework. To get the ID, the component must call `ctx.TrackResource`
during construction.

Finally, you'll need to provide an environment-agnostic constructor for your component:

```go
package mycomponent

func New(ctx resource.Context) (i Instance, err error){
    err = resource.UnsupportedEnvironment(ctx.Environment())
    ctx.Environment().Case(environment.Native, func() {
        i, err = newNative(ctx)
    })
    ctx.Environment().Case(environment.Kube, func() {
        i, err = newKube(ctx)
    })
    return
}

func NewOrFail(t test.Failer, ctx resource.Context) Instance {
    i, err := New(ctx)
    if err != nil {
        t.Fatal(err)
    }
    return i
}
```

Now that everything is in place, you can begin using your component:

```go
func TestMyLogic(t *testing.T) {
    framework.
        NewTest(t).
        Run(func(ctx framework.TestContext) {
            // Create the components.
            g := myComponent.NewOrFail(ctx, ctx)

            // Do more stuff...
        }
}
```

## Running Tests

The test framework builds on top of the Go testing infrastructure, and is therefore compatible with
the standard `go test` command-line.  For example, to run the tests under the `/tests/integration/mycomponent`
using the default (native) environment, you can simply type:

```console
$ go test -tags=integ ./tests/integration/mycomponent/...
```

Note that samples below invoking variations of ```go test ./...``` are intended to be run from the ```tests/integration``` directory.

Tests are tagged with the `integ` build target to avoid accidental invocation. If this is not set, no tests will be run.

### Test Parellelism and Kubernetes

By default, Go will run tests within the same package (i.e. suite) synchronously. However, tests in other packages
may be run concurrently.

When running in the Kubernetes environment this can be problematic for suites that deploy Istio. The Istio deployment,
as it stands is a singleton per cluster. If multiple suites attempt to deploy/configure Istio,
they can corrupt each other and/or simply fail.  To avoid this issue, you have a couple of options:

1. Run one suite per command (e.g. `go test ./tests/integration/mysuite/...`)
1. Disable parallelism with `-p 1` (e.g. `go test -p 1 ./...`). A major disadvantage to doing this is that it will also disable
parallelism within the suite, even when explicitly specified via [RunParallel](#parallel-tests).

### Test Selection

When no flags are specified, the test framework will run all applicable tests. It is possible to filter in/out specific
tests using 2 mechanisms:

1. The standard ```-run <regexp>``` flag, as exposed by Go's own test framework.
1. ```--istio.test.select <filter-expr>``` flag to select/skip framework-aware tests that use labels.

For example, if a test, or test suite uses labels in this fashion:

```go
func TestMain(m *testing.M) {
    framework.
        NewSuite("galley_conversion", m).
        // Test is tagged with "CustomSetup" label
        Label(label.CustomSetup).
        Run()
```

Then you can explicitly select execution of such tests using label based selection. For example, the following expression
will select only the tests that have the ```label.CustomSetup``` label.

```console
$ go test ./... --istio.test.select +customsetup
```

Similarly, you can exclude tests that use ```label.CustomSetup``` label by:

```console
$ go test ./... --istio.test.select -customsetup
```

You can "and" the predicates by separating with commas:

```console
$ go test ./... --istio.test.select +customsetup,-postsubmit
```

This will select tests that have ```label.CustomSetup``` only. It will **not** select tests that have both ```label.CustomSetup```
and ```label.Postsubmit```.

### Running Tests on CI

Istio's CI/CD system is composed of 2 parts:

Tool | Description |
---|---
[Prow](https://github.com/kubernetes/test-infra/tree/master/prow) | Kubernetes-based CI/CD system developed by the Kubernetes community and is deployed in Google Kubernetes Engine (GKE).
[TestGrid](https://k8s-testgrid.appspot.com/istio-release) | A Kubernetes dashboard used for visualizing the status of the Prow jobs.

Test suites are defined for each toplevel directory (such as `pilot` and `telemetry`), so any tests added to these directories will automatically be run in CI.

If you need to add a new test suite, it can be added to the [job configuration](https://github.com/istio/test-infra/blob/master/prow/config/jobs/istio.yaml).

### Running Tests on Custom Deployment

You can run integration tests againist a control plane deployed by another operator such as [sail-operator](https://github.com/istio-ecosystem/sail-operator):

With setting ```--istio.test.kube.controlPlaneInstaller=#{path/to/script}``` and ```--istio.test.kube.deploy=false``` you can skip internal istio operator installation and be able to call a script which lets you install another control plane.

> [!WARNING]
> Note that Istio does not perform any testing for this option in its CI nor maintains custom scripts or config files in its repository. You will be responsible to maintain your
> files in another repository.

#### External Installation Executable API

The External Installation Executable can be any executable, be it a script or a binary as long as the host OS can execute it. The script should take work directory as argument. The iop.yaml file under work directory which is generated by the integration test framework can be used to extract control plane values of the running test.

Examples will use `extinst` as script name.

##### 'install' command

Installs the Istio control plane for this test run. Takes 1 argument, work directory of integration test.

Example:
extinst install /work/test1/integ-test

##### 'cleanup' command

Cleans up the previously-installed control plane. Takes 1 argument, work directory of integration test.

Example:
extinst cleanup /work/test1/integ-test

## Environments

The test binaries run in a Kubernetes cluster, but the test logic runs in the test binary.

```console
$ go test ./... -p 1
```

| WARNING: ```-p 1``` is required when running directly in the ```tests/integration/``` folder. |
| --- |

You will need to provide a K8s cluster to run the tests against.
(See [here](https://github.com/istio/istio/blob/master/tests/integration/GKE.md)
for info about how to set up a suitable GKE cluster.)
You can specify the kube config file that should be used to use for connecting to the cluster, through
command-line:

```console
$ go test ./... -p 1 --istio.test.kube.config ~/.kube/config
```

If not specified, `~/.kube/config` will be used by default.

**Be aware that any existing content will be altered and/or removed from the cluster**.

Note that the HUB and TAG environment variables **must** be set when running tests in the Kubernetes environment.

## Diagnosing Failures

### Working Directory

The test framework will generate additional diagnostic output in its work directory. Typically, this is
created under the host operating system's temporary folder (which can be overridden using
the `--istio.test.work_dir` flag). The name of the work dir will be based on the test id that is supplied in
a tests TestMain method. These files typically contain some of the logging & diagnostic output that components
spew out as part of test execution

```console
$ go test galley/... --istio.test.work_dir /foo
  ...

$ ls /foo
  galley-test-4ef25d910d2746f9b38/

$ ls /foo/galley-test-4ef25d910d2746f9b38/
  istio-system-1537332205890088657.yaml
  ...
```

### Enabling CI Mode

When executing in the CI systems, the makefiles use the ```--istio.test.ci``` flag. This flag causes a few changes in
behavior. Specifically, more verbose logging output will be displayed, some of the timeout values will be more relaxed, and
additional diagnostic data will be dumped into the working directory at the end of the test execution.

The flag is not enabled by default to provide a better U/X when running tests locally (i.e. additional logging can clutter
test output and error dumping can take quite a while). However, if you see a behavior difference between local and CI runs,
you can enable the flag to make the tests work in a similar fashion.

### Preserving State (No Cleanup)

By default, the test framework will cleanup all deployed artifacts after the test run, especially on the Kubernetes
environment. You can specify the ```--istio.test.nocleanup``` flag to stop the framework from cleaning up the state
for investigation.

### Additional Logging

The framework accepts standard istio logging flags. You can use these flags to enable additional logging for both the
framework, as well as some of the components that are used in-line in the native environment:

```console
$ go test ./... --log_output_level=tf:debug
```

The above example will enable debugging logging for the test framework (```tf```) and the MCP protocol stack (```mcp```).

### Running Tests Under Debugger (GoLand)

The tests authored in the new test framework can be debugged directly under GoLand using the debugger. If you want to
pass command-line flags to the test while running under the debugger, you can use the
[Run/Debug configurations dialog](https://i.stack.imgur.com/C6y0L.png) to specify these flags as program arguments.

## Reference

### Command-Line Flags

The test framework supports the following command-line flags:

| Name | Type | Description |
|------|------|-------------|
| --istio.test.work_dir | string | Local working directory for creating logs/temp files. If left empty, os.TempDir() is used. |
| --istio.test.ci | bool | Enable CI Mode. Additional logging and state dumping will be enabled. |
| --istio.test.nocleanup | bool | Do not cleanup resources after test completion. |
| --istio.test.select | string | Comma separated list of labels for selecting tests to run (e.g. 'foo,+bar-baz'). |
| --istio.test.hub | string | Container registry hub to use (default HUB environment variable). |
| --istio.test.tag | string | Common Container tag to use when deploying container images (default TAG environment variable). |
| --istio.test.pullpolicy | string | Common image pull policy to use when deploying container images. |
| --istio.test.kube.config | string | A comma-separated list of paths to kube config files for cluster environments. (default ~/.kube/config). |
| --istio.test.kube.deploy | bool | Deploy Istio into the target Kubernetes environment. (default true). |
| --istio.test.kube.deployEastWestGW | bool | Deploy Istio east west gateway into the target Kubernetes environment. (default true). |
| --istio.test.kube.systemNamespace | string | The namespace where the Istio components reside in a typical deployment. (default "istio-system"). |
| --istio.test.kube.helm.values | string | Manual overrides for Helm values file. Only valid when deploying Istio. |
| --istio.test.kube.helm.iopFile | string | IstioOperator spec file. This can be an absolute path or relative to the repository root. Defaults to "tests/integration/iop-integration-test-defaults.yaml". |
| --istio.test.kube.loadbalancer | bool | Used to obtain the right IP address for ingress gateway. This should be false for any environment that doesn't support a LoadBalancer type. |
| --istio.test.kube.deployGatewayAPI | bool | Deploy gateway API during tests execution. (default is "true"). |
| --istio.test.revision | string | Overwrite the default namespace label (istio-enabled=true) with revision lable (istio.io/rev=XXX). (default is no overwrite). |
| --istio.test.skip | []string | Skip tests matching the regular expression. This follows the semantics of -test.run. |
| --istio.test.skipVM | bool | Skip all the VM related parts in all the tests. (default is "false"). |
| --istio.test.helmRepo | string | Overwrite the default helm Repo used for the tests. |
| --istio.test.ambient | bool | Indicate the use of ambient mesh. |
| --istio.test.openshift | bool | Set to `true` when running the tests in an OpenShift cluster, rather than in KinD. |

## Notes

### Running on a Mac

* Currently some _native_ tests fail when being run on a Mac with an error like:

```plain
unable to locate an Envoy binary
```

This is documented in this [PR](https://github.com/istio/istio/issues/13677). Once the Envoy binary is available for the Mac,
these tests will hopefully succeed.

* If one uses Docker for Mac for the kubernetes environment be sure to specify the `-istio.test.kube.loadbalancer=false` parameter. This solves an error like:

```plain
service ingress is not available yet
```
