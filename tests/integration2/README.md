# Istio Integration Tests

This folder contains Istio integration tests that use the test framework checked in at 
[istio.io/istio/pkg/test/framework](https://github.com/istio/istio/tree/master/pkg/test/framework).

## Table of Contents
1. [Basics](#basics)
    1. [Environments](#environments)
        1. [Kubernetes](#kubernetes-environment)
3. [Adding New Tests](#adding-new-tests)
4. [Diagnosing Failures](#diagnosing-failures)
5. [Reference](#reference)
    1. [Commandline Flags](#command-line-flags)


## Basics

The goal of the framework is to make it as easy as possible to author and run tests. In its simplest
case, just typing ```go test ./...``` should be sufficient to run tests.

The test framework is designed to work with standard go tooling and allows developers
to write environment-agnostics tests in a high-level fashion. The quickest way to get started with authoring
new tests is to checkout the code in the
[examples](https://github.com/istio/istio/tree/master/tests/integration2/examples) folder.

The test framework has various flags that can be set to change its behavior.

 
### Environments

The test framework currently supports two environments:
 
  * **Local**: The test binaries run either in-memory, or locally as processes.
  * **Kubernetes**: The test binaries run in a Kubernetes cluster, but the test logic runs in the test binary.
  
When running tests, only one environment can be specified:

```console
$ go test ./... -istio.test.env local
$ go test ./... -istio.test.env kubernetes
```

By default, the tests will be executed in the
specified environment. However, test authors can specifically indicate a particular environment as
a requirement for their tests. If the indicated environment does not match the environment that is required
by the test, then the test will be skipped.

```go
// Will be skipped in kubernetes environment.
func TestLocalOnly(t *testing.T) {
	framework.Requires(t, dependency.Local)
	// ...
}
```

When an environment is not specified, the tests will run against the local environment by default.

#### Kubernetes Environment

When running the tests against the Kubernetes environment, you will need to explicitly specify a kube config
file for the cluster you need to use. **Be aware that any existing Istio deployment will be altered and/or
removed**.

```console
$ go test ./...  -istio.test.env kubernetes -istio.test.kube.config ~/.kube/config
```



## Adding New Tests

Please follow the general guidance for adding new tests:

 * Create a new top-level folder for top-level components (i.e. mixer, pilot galley).

```console 
$ cd ${ISTIO}/tests/integration2
$ mkdir mycomponent
```

 * In that folder, create a new Go test with a TestMain specified:
 
 ```go
func TestMain(m *testing.M) {
	framework.Run("mycomponent_test", m)
}
 ```
 
  * Add your folder name to the top-level 
  [tests.mk](https://github.com/istio/istio/tree/master/tests/integration2/tests.mk) to file get it included 
  in make targets and check-in gates:
  
```make
_INTEGRATION_TEST_NAMES = galley mixer mycomponent
``` 


## Diagnosing Failures

The test framework will generate additional diagnostic output in its work directory. Typically, this is 
created under the host operating system's temporary folder (which can be overriden using 
```--istio.test.work_dir``` flag). The name of the work dir will be based on the test id that is supplied in
a tests TestMain method.

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



## Reference


### Command-Line Flags

The test framework supports the following command-line flags:

```
Common Flags:
--istio.test.work_dir <dir>      Local working directory for creating logs/temp files.
--istio.test.env <envname>       Specify the environment to run the tests against.

Kubernetes Environment Flags:
--istio.test.kube.config <path>   The path to the kube config file for cluster environments
--istio.test.kube.hub <string>    The hub for docker images.
--istio.test.kube.tag <string>    The tag for docker images.
--istio.test.kube.testNamespace   The namespace for each individual test. 
--istio.test.kube.systemNamespace The namespace where the Istio components reside in a typical deployment.
--istio.test.kube.dependencyNamespace  The namespace in which dependency components are deployed.    
```