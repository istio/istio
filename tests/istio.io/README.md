# Istio.io Example Tests

## Overview

This folder contains tests for the different examples on Istio.io.

## Test functions

The Istio.io test framework supports the following test functions:

- You can use `RunScript` to execute scripts.
- You can use `Apply` to apply YAML files to a namespace and `Delete` to remove them.
- You can use `RunScript` to execute scripts.
- You can `WaitForPods` to wait for pods to deploy.
- You can use `Exec` to execute custom go functions to.



### Executing Scripts

You can execute scripts using the `RunScript(script string, output outputType, verifier VerificationFunction)` method. This method takes the following parameters:

- A path to a script starting at the root of the Istio source tree
- An output type:`TextOutput`,`YamlOutput`, or `JsonOutput`
- An optional verification function

If you don't provide a verification function, then error codes the script returns are evaluated as test failures.

This example performs the following actions:

- Execute the `curl-foo-bar-legacy.sh` script
- Store the output in the `curl-foo-bar-legacy.sh_output.txt` file
- Verify that there are three responses, each with a `200` return code
`curl-foo-bar-legacy.sh_output.txt`, and verify that there are three responses, each with a return code of 200.


```golang
func TestMTLS(t *testing.T) {
  examples.New(t, "mtls-migration").
    RunScript("curl-foo-bar-legacy.sh", examples.TextOutput, examples.GetCurlVerifier([]string{"200", "200", "200"})).
    Run()
}
```

The `curl-foo-bar-legacy.sh_output.txt` contains the following output:

```bash
sleep.foo to httpbin.foo: 200
sleep.bar to httpbin.foo: 200
sleep.legacy to httpbin.foo: 200
```

### Applying/Deleting YAML files

You can use `Apply(namespace string, path string)` to run `kubectl apply -f` on a namespace using the specified YAML file. You can also use `Delete(namespace string, path string)` to run `kubectl delete -f` and remove the configuration specified in the YAML file from the namespace. In both cases, the paths are relative to the root of the Istio source tree.

This example applies the `samples/bookinfo/platform/kube/rbac/productpage-policy.yaml` YAML file on the `bookinfo` namespace:

```golang
func enforcingServiceLevelAccessControlStep1(t *testing.T) {
	examples.New(t, "Enforcing Service-level access control Step 1").
		Apply("default", "samples/bookinfo/platform/kube/rbac/productpage-policy.yaml").
		Run()
}
```

### Executing Go functions

You can use the `Exec(testFunction testFunc)` method to run custom tests written in Go. The function you pass as a parameter must have the `func(t *testing.T) error` form.

As an example, the following example would cause a test to fail if Minikube is used.

```golang
examples.New(t, "this test fails on Minikube").
		Exec(func(t *testing.T) error {
			if minikubeEnabled {
				t.Fatal("This test doesn't work on Minikube.")
			}
			return nil
		})
```

### Waiting for Pods to Deploy

You can use the `WaitForPods(fetchFunc KubePodFetchFunc)` function to wait for pods to deploy. The `KubePodFetchFunc` fetch function you pass as a parameter must have the `func(env *kube.Environment) kubePkg.PodFetchFunc` form.

The `WaitForPods` function allows the test to wait for the set of pods you specify in the fetch function to start, or for 30 seconds, whichever comes first. The next example performs the following actions:

1. Runs the `create-ns-foo-bar-legacy.sh` script to create several pods in the `foo`, `bar`, and `legacy` namespaces.
2. Calls the `WaitForPods` function with the built-in `examples.NewMultiPodFetch` to select all pods from the `foo` namespace.

```golang
func TestMTLS(t *testing.T) {
  examples.New(t, "mtls-migration").
    RunScript("create-ns-foo-bar-legacy.sh", examples.TextOutput, nil).
		WaitForPods(examples.NewMultiPodFetch("foo")).
    Run()
}
```

## Writing a test

To write a test for content in istio.io follow these steps:

1. Add the following imports to your GoLang file:

```golang
"istio.io/istio/pkg/test/framework"
"istio.io/istio/pkg/test/framework/components/environment"
"istio.io/istio/pkg/test/framework/components/istio"
"istio.io/istio/pkg/test/istio.io/examples"
```

2. Create a function called `TestMain`, following the example below. This function sets up the Istio environment that the test uses. The `Setup` function accepts an optional function to customize the Istio environment deployed.

```golang
func TestMain(m *testing.M) {
  framework.NewSuite("mtls-migration", m).
    SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
    RequireEnvironment(environment.Kube).
    Run()
}
```

The below example shows how to use a `Setup` function to set the `minikubEnabled` variable if `Minikube` is used for tests.

```golang
var (
  minikubeEnabled bool
)

func TestMain(m *testing.M) {
  framework.NewSuite("mtls-migration", m).
    SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
    RequireEnvironment(environment.Kube).
    Setup(func ctx resource.Context) (err error) {
      minikubeEnabled = ctx.Environment().(*kube.Environment).Settings().Minikube
    }
    Run()
}
```

3. To create the function for your test, you can combine the test functions above, for example:

```golang
func TestMTLS(t *testing.T) {
  examples.New(t, "mtls-migration").
    RunScript("create-ns-foo-bar-legacy.sh", examples.TextOutput, nil).
    WaitForPods(examples.NewMultiPodFetch("foo")).
    RunScript("curl-foo-bar-legacy.sh", examples.TextOutput, examples.GetCurlVerifier([]string{"200", "200", "200"})).
    Run()
}
```

This example performs the following actions:
- Executes the `create-ns-foo-bar-legacy.sh` script, which creates pods in the `foo`, `bar`, and `legacy` namespaces.
- `WaitForPods` uses the `NewMultiPodFetch` function to wait for all pods in the `foo` namespace to start.
- Calls the `curl-foo-bar-legacy.sh` function, which uses the `GetCurlVerifier` function to verify the responses from the curl script.
- Finally, `Run` is called to execute the test.

### Built in Verifiers

The framework includes some predefined verifiers for scripts. These are described below.

* `examples.GetCurlVerifier`: The GetCurlVerifier method is used to create a verifier that parses the output of a curl script and compares each response. If a response of `000` is returned from curl, the verifier will compare the next line against the provided output. Finally, the run method triggers the test to run. This validator expects responses in the form `sleep.foo to httpbin.foo: 200` except for when the response code is 000, in which case, the next line of output is expected to be an error statement. For example, the following output can be verified with `examples.GetCurlVerifier([]string{"200", "200", "000", "command terminated with exit code 56"}))`.

```bash
sleep.foo to httpbin.foo: 200
sleep.bar to httpbin.foo: 200
sleep.legacy to httpbin.foo: 000
command terminated with exit code 56
```

### Output

When the test framework runs, it creates a directory called output. This directory contains a copy of all YAML files, scripts, and a file for each script containing the output.

## Executing tests using Make test

You can execute all istio.io tests using make.

* Create a Minikube environment as advised on istio.io
* Tests can be run using the following commands:

```bash
export KUBECONFIG=~/.kube/config
make istioio-test
```

## Executing individual tests

You can execute individual tests using Go test as shown below.

* Create a Minikube environment as advised on istio.io
* Tests can be run using the below command.

```bash
 go test ./... -p 1 --istio.test.env kube -v
```

In this command, `istio.test.env kube` specifies that tests should be executed in a Kubernetes environment, `-p` disables parallelization, and `-v` prints the output even for passing tests. More arguments can be found on [this wiki page](https://github.com/istio/istio/wiki/Istio-Test-Framework).
