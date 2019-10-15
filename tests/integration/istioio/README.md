# Istio.io Content Test Framework and Examples

This folder contains the tests for the content on [istio.io](http://istio.io).

## Test functions

The `istio.io` test framework supports the following test functions:

- You can use `RunScript` to execute scripts.
- You can use `Apply` to apply YAML files to a namespace and `Delete` to remove them.
- You can use `RunScript` to execute scripts.
- You can `WaitForPods` to wait for pods to deploy.
- You can use `Exec` to execute custom go functions to.

### File Selectors

Many of the methods take an argument of type `istioio.FileSelector`:

```golang
type FileSelector interface {
    SelectFile(ctx Context) string
}
```

This interface is executed at runtime and provides the test `istioio.Context`,
which provides the ability to dynamically choose which file to use. For example,
we could choose a different file if we are running on Minikube:

```golang
istioio.FileFunc(func(ctx istioio.Context) string {
    if ctx.Env.Settings().Minikube {
        return "scripts/curl-httpbin-tls-gateway-minikube.sh"
    }
    return "scripts/curl-httpbin-tls-gateway-gke.sh"
}
```

This is a more advanced example, however. Typically, the file used will be constant.
To help with these cases, the framework provides the following utilities:

- `istioio.File`: is a string constant `FileSelector` implementation. Files can be
absolute or relative to the working directory.
- `istioio.IstioSrc`: is like `istioio.File` except that the value is assumed to be
relative to the Istio source directory (`$GOPATH/src/istio.io/istio`).

### Execute scripts

You can execute scripts using the following function:

```golang
RunScript(script istioio.FileSelector, verifier istioio.Verifier)
```

This function takes the following parameters:

- A file selector for the script.
- An optional verification function

If you don't provide a verification function, then the error codes that the
script returns are evaluated as test failures.

This example performs the following actions:

- Execute the `curl-foo-bar-legacy.sh` script
- Store the output in the `curl-foo-bar-legacy.sh_output.txt` file
- Verify that there are three responses, each with a `200` return code in
  `curl-foo-bar-legacy.sh_output.txt`

```golang
func TestScript(t *testing.T) {
    framework.
        NewTest(t).
        Run(istioio.NewBuilder().
            RunScript(istioio.File("curl-foo-bar-legacy.sh"), istioio.CurlVerifier("200", "200", "200")).
            Build())
}
```

The `curl-foo-bar-legacy.sh_output.txt` contains the following output:

```bash
sleep.foo to httpbin.foo: 200
sleep.bar to httpbin.foo: 200
sleep.legacy to httpbin.foo: 200
```

### Apply or delete YAML files

You can use the `Apply(namespace string, path istioio.FileSelector)` function to run the
`kubectl apply -f` command on a namespace using the specified YAML file. You can
also use the `Delete(namespace string, path istioio.FileSelector)` function to run the
`kubectl delete -f` command and remove the configuration specified in the YAML
file from the namespace. In both cases, the paths are relative to the root of
the Istio source tree.

This example applies the
`samples/bookinfo/platform/kube/rbac/productpage-policy.yaml` YAML file on the
`bookinfo` namespace:

```golang
func TestApply(t *testing.T) {
    framework.
        NewTest(t).
        Run(istioio.NewBuilder().
            Apply("default", istioio.IstioSrc("samples/bookinfo/platform/kube/rbac/productpage-policy.yaml")).
            Build())
}
```

### Execute Go functions

You can use the `Exec(testFunction testFunc)` function to run custom tests
written in Go. The function you pass as a parameter must have the
`func(t *testing.T) error` form.

As an example, the following example test fails if you use Minikube.

```golang
framework.
    NewTest(t).
    Run(istioio.NewBuilder().
        Exec(func(ctx istioio.Context) error {
            if ctx.Env.Settings().Minikube {
                t.Fatal("This test doesn't work on Minikube.")
            }
            return nil
        })
        Build())
```

### Waiting for Pods to Deploy

You can use the `WaitForPods(fetchFunc KubePodFetchFunc)` function to wait for
pods to deploy. The `KubePodFetchFunc` fetch function you pass as a parameter
must have the `func(ctx istioio.Context) kubePkg.PodFetchFunc` form.

The `WaitForPods` function allows the test to wait for the set of pods you
specify in the fetch function to start, or for 30 seconds, whichever comes
first. The next example performs the following actions:

1. Runs the `create-ns-foo-bar-legacy.sh` script to create several pods in the
   `foo`, `bar`, and `legacy` namespaces.
1. Calls the `WaitForPods` function with the built-in
   `istioio.NewMultiPodFetch` to select all pods from the `foo` namespace.

```golang
func TestPodFetch(t *testing.T) {
    framework.
        NewTest(t).
        Run(istioio.NewBuilder().
            RunScript(istioio.File("create-ns-foo-bar-legacy.sh"), nil).
            WaitForPods(istioio.NewMultiPodFetch("foo")).
            Build())
}
```

## Writing a test

To write a test for content in istio.io follow these steps:

1. Add the following imports to your GoLang file:

```golang
"istio.io/istio/pkg/test/framework"
"istio.io/istio/pkg/test/framework/components/environment"
"istio.io/istio/pkg/test/framework/components/istio"
"istio.io/istio/pkg/test/istioio"
```

1. Create a function called `TestMain`, following the example below. This
   function sets up the Istio environment that the test uses. The `Setup`
   function accepts an optional function to customize the Istio environment
   deployed.

```golang
func TestMain(m *testing.M) {
framework.NewSuite("my-istioio-test", m).
    SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
    RequireEnvironment(environment.Kube).
    Run()
}
```

1. To create the function for your test, you can combine the test functions
   above, for example:

```golang
func TestCombinedMethods(t *testing.T) {
    framework.
        NewTest(t).
        Run(istioio.NewBuilder().
            RunScript(istioio.File("create-ns-foo-bar-legacy.sh"), nil).
            WaitForPods(istioio.NewMultiPodFetch("foo")).
            RunScript(istioio.File("curl-foo-bar-legacy.sh"), istioio.GetCurlVerifier("200", "200", "200")).
            Build())
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

The framework includes some predefined verifiers for scripts. For example, the
`istio.CurlVerifier` function. You can use this function to
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
istioio.CurlVerifier("200", "200", "000", "command
terminated with exit code 56"))
```

### Output

When the test framework runs, it creates the `output` directory in the working directory
of the test. This directory contains a copy of all YAML files, scripts, and output files
with the output of each script.

## Execute tests with `make test`

You can execute all istio.io tests using make.

- Create a Minikube environment as [advised on istio.io](https://istio.io/docs/setup/platform-setup/minikube/)
- Run the tests using the following commands:

```bash
export KUBECONFIG=~/.kube/config
make test.integration.istioio.kube.presubmit
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
