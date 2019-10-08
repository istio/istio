# Istio.io Content Test Framework and Examples

This folder contains the tests for the content on Istio.io.

## Test functions

The Istio.io test framework supports the following test functions:

- You can use `RunScript` to execute scripts.
- You can use `Apply` to apply YAML files to a namespace and `Delete` to remove them.
- You can use `RunScript` to execute scripts.
- You can `WaitForPods` to wait for pods to deploy.
- You can use `Exec` to execute custom go functions to.

### Execute scripts

You can execute scripts using the following function:

```golang
RunScript(script string, output outputType, verifier VerificationFunction)
```

This function takes the following parameters:

- A path to a script starting at the root of the Istio source tree
- An output type:`TextOutput`,`YamlOutput`, or `JsonOutput`
- An optional verification function

If you don't provide a verification function, then the error codes that the
script returns are evaluated as test failures.

This example performs the following actions:

- Execute the `curl-foo-bar-legacy.sh` script
- Store the output in the `curl-foo-bar-legacy.sh_output.txt` file
- Verify that there are three responses, each with a `200` return code in
  `curl-foo-bar-legacy.sh_output.txt`

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

### Apply or delete YAML files

You can use the `Apply(namespace string, path string)` function to run the
`kubectl apply -f` command on a namespace using the specified YAML file. You can
also use the `Delete(namespace string, path string)` function to run the
`kubectl delete -f` command and remove the configuration specified in the YAML
file from the namespace. In both cases, the paths are relative to the root of
the Istio source tree.

This example applies the
`samples/bookinfo/platform/kube/rbac/productpage-policy.yaml` YAML file on the
`bookinfo` namespace:

```golang
func enforcingServiceLevelAccessControlStep1(t *testing.T) {
    examples.New(t, "Enforcing Service-level access control Step 1").
        Apply("default", "samples/bookinfo/platform/kube/rbac/productpage-policy.yaml").
        Run()
}
```

### Execute Go functions

You can use the `Exec(testFunction testFunc)` function to run custom tests
written in Go. The function you pass as a parameter must have the
`func(t *testing.T) error` form.

As an example, the following example test fails if you use Minikube.

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

You can use the `WaitForPods(fetchFunc KubePodFetchFunc)` function to wait for
pods to deploy. The `KubePodFetchFunc` fetch function you pass as a parameter
must have the `func(env *kube.Environment) kubePkg.PodFetchFunc` form.

The `WaitForPods` function allows the test to wait for the set of pods you
specify in the fetch function to start, or for 30 seconds, whichever comes
first. The next example performs the following actions:

1. Runs the `create-ns-foo-bar-legacy.sh` script to create several pods in the
   `foo`, `bar`, and `legacy` namespaces.
2. Calls the `WaitForPods` function with the built-in
   `examples.NewMultiPodFetch` to select all pods from the `foo` namespace.

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

2. Create a function called `TestMain`, following the example below. This
   function sets up the Istio environment that the test uses. The `Setup`
   function accepts an optional function to customize the Istio environment
   deployed.

    ```golang
    func TestMain(m *testing.M) {
    framework.NewSuite("mtls-migration", m).
        SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
        RequireEnvironment(environment.Kube).
        Run()
    }
    ```

    The following example shows how to use a `Setup` function to set the
    `minikubEnabled` variable if `Minikube` is used for tests.

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

3. To create the function for your test, you can combine the test functions
   above, for example:

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

- Executes the `create-ns-foo-bar-legacy.sh` script, which creates pods in the
  `foo`, `bar`, and `legacy` namespaces.
- `WaitForPods` uses the `NewMultiPodFetch` function to wait for all pods in the
  `foo` namespace to start.
- Calls the `curl-foo-bar-legacy.sh` script, which uses the `GetCurlVerifier`
  function to verify the responses from the curl script.
- Finally, `Run` is called to execute the test.

### Built in Verifiers

The framework includes some predefined verifiers for scripts. Fore example, the
`examples.GetCurlVerifier` function. You can use this function to
create a verifier that parses the output of a curl script and compares each
response. If curl returns `000`, the verifier compares the next line against the
provided output. The validator expects responses in the form of
`sleep.foo to httpbin.foo: 200` except for `000` responses. For `000` responses,
the validator expects the next line of output to be an error statement.

For example, to verify this output:

```bash
sleep.foo to httpbin.foo: 200
sleep.bar to httpbin.foo: 200
sleep.legacy to httpbin.foo: 000
command terminated with exit code 56
```

You can use the following function:

```golang
examples.GetCurlVerifier([]string{"200", "200", "000", "command
terminated with exit code 56"}))
```

### Output

When the test framework runs, it creates the `output` directory. This directory
contains a copy of all YAML files, scripts, and output files with the output of
each script.

## Execute tests with `make test`

You can execute all istio.io tests using make.

- Create a Minikube environment as [advised on istio.io](https://istio.io/docs/setup/platform-setup/minikube/)
- Run the tests using the following commands:

```bash
export KUBECONFIG=~/.kube/config
make istioio-test
```

## Execute individual tests

You can execute individual tests using Go test as shown below.

- Create a Minikube environment as [advised on istio.io](https://istio.io/docs/setup/platform-setup/minikube/)
- Run the test using the following command:

```bash
 go test ./... -p 1 --istio.test.env kube -v
```

In this command, `istio.test.env kube` specifies that the test should execute
in a Kubernetes environment, `-p` disables parallelization, and `-v` prints the
output even for passing tests.

You can find the complete list of arguments on [the test framework wiki page](https://github.com/istio/istio/wiki/Istio-Test-Framework).
