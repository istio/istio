# Istio Integration Tests

This folder contains Istio integration tests that use the test framework checked in at 
[istio.io/istio/pkg/test/framework](https://github.com/istio/istio/tree/master/pkg/test/framework).

### Basics

The goal of the framework is to make it as easy to author and run tests as possible. In it is most simplest
case, just typing ```go test ./...``` should be sufficient to run tests.

The test framework is designed to work with standard go tooling and allows developers
to write environment-agnostics tests in a high-level fashion. The quickest way to get started with authoring
new tests is to checkout the code in 
[examples](https://github.com/istio/istio/tree/master/tests/integration2/examples) folder.

The test framework has various flags that can be set to change its behavior.

 
### Environments

The test framework currently support two environments:
 
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
file for the cluster you need to use. **Be aware that the target cluster will be changed destructively**.

```console
$ go test ./...  -istio.test.env kubernetes -istio.test.kube.config ~/.kube/config
```

### Adding New Tests

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
INTEGRATION_TEST_NAMES = galley mixer mycomponent
``` 
