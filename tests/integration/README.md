# Istio Integration Tests

This folder contains Istio integration tests that use the test framework checked in at 
[istio.io/istio/pkg/test/framework](https://github.com/istio/istio/tree/master/pkg/test/framework).

## Table of Contents
1. [Basics](#basics)
    1. [Environments](#environments)
        1. [Kubernetes](#kubernetes-environment)
    2. [Test Selection](#test-selection)
    3. [Test Lifecycle](#test-lifecycle)
2. [Diagnosing Failures](#diagnosing-failures)
    1. [Working Directory](#working-directory)
    2. [Enabling CI Mode](#enabling-ci-mode)
    3. [Preserving State (No Cleanup)](#preserving-state-no-cleanup)
    4. [Additional Logging](#additional-logging)
    5. [Running Tests Under Debugger](#running-tests-under-debugger-goland)
3. [Adding New Tests](#adding-new-tests)
   1. [Introducing Your Suite](#introducing-your-suite)
4. [Reference](#reference)
    1. [Commandline Flags](#command-line-flags)


## Basics

The goal of the framework is to make it as easy as possible to author and run tests. In its simplest
case, just typing ```go test ./...``` should be sufficient to run tests.

The test framework is designed to work with standard go tooling and allows developers
to write environment-agnostics tests in a high-level fashion. The quickest way to get started with authoring
new tests is to checkout the code in the
[examples](https://github.com/istio/istio/tree/master/tests/integration/examples) folder.

The test framework has various flags that can be set to change its behavior.

 
### Environments

The test framework currently supports two environments:
 
  * **Local**: The test binaries run either in-memory, or locally as processes.
  * **Kubernetes**: The test binaries run in a Kubernetes cluster, but the test logic runs in the test binary.
  
When running tests, only one environment can be specified:

```console
$ go test ./... -istio.test.env native
$ go test ./... -p 1 -istio.test.env kube
```

| WARNING: ```-p 1``` is required when running directly in the ```tests/integration/``` folder, when using ```kube``` environment. |
| --- |

Most tests will support execution in both modes, however, some tests maybe tagged to run on a specific environment.
When executed a non-supported environment, these tests will be skipped. 

When an environment is not specified, the tests will run against the local environment by default.

#### Kubernetes Environment

When running the tests against the Kubernetes environment, you will need to provide a K8s cluster to run the tests
against. You can specify the kube config file that should be used to use for connecting to the cluster, through 
command-line: 

```console
$ go test ./...  --istio.test.env kube --istio.test.kube.config ~/.kube/config
```

If not specified, ~/.kube/config will be used by default.

**Be aware that any existing content will be altered and/or removed from the cluster**.

### Test Selection

When no flags are specified, the test framework will run all applicable tests. It is possible to filter in/out specific
tests using 2 mechanisms:
  
  1. The standard ```-run <regexp>``` flag, as exposed by Go's own test framework.
  2. ```--istio.test.select <filter-expr>``` flag to select/skip framework-aware tests that use labels.
  
For example, if a test, or test suite uses labels in this fashion:

```go
func TestMain(m *testing.M) {
	framework.
		NewSuite("galley_conversion", m).
		// Test is tagged with "Presubmit" label
		Label(label.Presubmit).
		Run()
``` 

Then you can explicitly select execution of such tests using label based selection. For example, the following expression
will select only the tests that have the ```label.Presubmit``` label.
```console
$ go test ./... --istio.test.select +presubmit
```

Similarly, you can exclude tests that use ```label.Presubmit``` label by:
```console
$ go test ./... --istio.test.select -presubmit
```

You can "and" the predicatesby seperating commas:
```console
$ go test ./... --istio.test.select +presubmit,-postsubmit
```
This will select tests that have ```label.Presubmit``` only. It will **not** select tests that have both ```label.Presubmit```
and ```label.Postsubmit```.

### Test Lifecycle 

Integration tests are prone to be flaky. Introducing a suite or a test from the get-go can cause sporadic failures in the
check-in queues. Introducing your tests in a gradual manner allows you to deal with the flakes and sporadic
failures without disrupting other people's work.

#### Add Your Suite
As first step, add your test suite and tests **without using any labels**. This will cause these tests to be picked up and
run as part of all unstable tests automatically

#### Observe, Fix Flakes, Stabilize
Once checked-in, you can observe what is going on with your tests at the
[TestGrid](https://testgrid.k8s.io/istio-presubmits) website. It is crucial to follow how your tests are doing and fix any 
flakes or issues as early as possible.

Once it has run in the check-in queues stably for enough time (i.e. 1 week), then it is ready to be promoted to be a
presubmit gate.

#### Promotion To Stable/Presubmit

Tag your test or suite with the ```label.Presubmit```. This will cause the presubmit gates to automatically pick-up your
test and start running as part of a required gate.
  

## Diagnosing Failures


### Working Directory

The test framework will generate additional diagnostic output in its work directory. Typically, this is 
created under the host operating system's temporary folder (which can be overridden using 
```--istio.test.work_dir``` flag). The name of the work dir will be based on the test id that is supplied in
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

In CircleCI, these files can be found in the artifacts section on the test job page:

![CircleCI Artifacts Tab Screenshot](https://circleci.com/docs/assets/img/docs/artifacts.png)


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
$ go test ./... --log_output_level=tf:debug,mcp:debug
``` 

The above example will enable debugging logging for the test framework (```tf```) and the MCP protocol stack (```mcp```).


### Running Tests Under Debugger (GoLand)

The tests authored in the new test framework can be debugged directly under GoLand using the debugger. If you want to
pass command-line flags to the test while running under the debugger, you can use the 
[Run/Debug configurations dialog](https://i.stack.imgur.com/C6y0L.png) to specify these flags as program arguments.


## Adding New Tests

### Introducing Your Suite

This section quickly introduces how you can add your own suite and tests. You can find a more comprehensive example in
the [examples](https://github.com/istio/istio/tree/master/tests/integration/examples) folder.

* Create a new top-level folder for top-level components (i.e. mixer, pilot, galley). This will automatically add the 
suite to be added to the list of suites that get executed in various CI gates.

```console 
$ cd ${ISTIO}/tests/integration
$ mkdir mycomponent
```

* In that folder, create a new Go test with a TestMain specified:

```go
func TestMain(m *testing.M) {
  framework.Run("mycomponent_test", m)
}
 ```
 
 In the main, you can also restrict the test to particular environment, apply labels, or do test-wide setup, such as
 deploying Istio.
 
```go
func TestMain(m *testing.M) {
	framework.
		NewSuite("mycomponent_test", m).
		// Restrict the test to the K8s environment only, tests will be skipped in native environment.
		RequireEnvironment(environment.Kube).
		// Deploy Istio on the cluster
		Setup(istio.SetupOnKube(nil, nil)).
		// Run your own custom setup
		Setup(mySetup).
		Run()
}

func mySetup(ctx framework.SuiteContext) error {
    // Your own setup code	
}
 ```
 
## Reference

### Helm Values Overrides

If your tests require special Helm values flags, you can specify your Helm values via additional
for Kubernetes environments. See [mtls_healthcheck_test.go](security/healthcheck/mtls_healthcheck_test.go) for example.


### Command-Line Flags

The test framework supports the following command-line flags:

```
  -istio.test.env string
    	Specify the environment to run the tests against. Allowed values are: [native kube] (default "native")

  -istio.test.work_dir string
    	Local working directory for creating logs/temp files. If left empty, os.TempDir() is used. (default "/var/folders/x0/c473mbq9269262gd1zj0lwt8008pwc/T/")

 -istio.test.ci
    	Enable CI Mode. Additional logging and state dumping will be enabled.

  -istio.test.nocleanup
    	Do not cleanup resources after test completion
    	    	
  -istio.test.select string
    	Comma separatated list of labels for selecting tests to run (e.g. 'foo,+bar-baz').

  -istio.test.hub string
    	Container registry hub to use (default "gcr.io/oztest-mixer")
    	
  -istio.test.tag string
    	Common Container tag to use when deploying container images (default "ozevren")
    	    	    	
  -istio.test.pullpolicy string
    	Common image pull policy to use when deploying container images
    	
  -istio.test.kube.config string
    	The path to the kube config file for cluster environments
    	
  -istio.test.kube.deploy
    	Deploy Istio into the target Kubernetes environment. (default true)
    	
  -istio.test.kube.deployTimeout duration
    	Timeout applied to deploying Istio into the target Kubernetes environment. Only applies if DeployIstio=true.
    	
  -istio.test.kube.undeployTimeout duration
    	Timeout applied to undeploying Istio from the target Kubernetes environment. Only applies if DeployIstio=true.
    	
  -istio.test.kube.systemNamespace string
    	The namespace where the Istio components reside in a typical deployment. (default "istio-system")

  -istio.test.kube.helm.chartDir string
    	Helm chart dir for Istio. Only valid when deploying Istio. (default "/Users/ozben/go/src/istio.io/istio/install/kubernetes/helm/istio")
    	
  -istio.test.kube.helm.values string
    	Manual overrides for Helm values file. Only valid when deploying Istio.
    	
  -istio.test.kube.helm.valuesFile string
    	Helm values file. This can be an absolute path or relative to chartDir. Only valid when deploying Istio. (default "test-values/values-e2e.yaml")
    	
  -istio.test.kube.minikube
    	Indicates that the target environment is Minikube. Used by Ingress component to obtain the right IP address..
    	    

```

### Testing Apps

Testing application implementations can be found at [`pkg/test/application`](https://github.com/istio/istio/tree/master/pkg/test/application).

Kubernetes environment `Apps` component allows cutomized [configuration](https://github.com/istio/istio/tree/master/tests/integration2/security/healthcheck/mtls_healthcheck_test.go) to only deploy the apps you need.
