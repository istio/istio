# Testing istio.io Content

This folder contains tests for the content on [istio.io](http://istio.io).

The content on `istio.io` will be generated from the output of these tests.
This means that we verify that the content actually works before we publish it.

These tests use the framework defined in the `istioio` package, which is a thin wrapper
around the [Istio test framework](https://github.com/istio/istio/wiki/Istio-Test-Framework).

## Output

When you run an `istio.io` test, it outputs snippets according to the
[istio.io syntax](https://istio.io/about/contribute/creating-and-editing-pages) and are ready for
import to `istio.io`. For example:

```text
$snippet enabling_istio_authorization.sh syntax="bash"
$ kubectl apply -f @samples/bookinfo/platform/kube/rbac/rbac-config-ON.yaml@
$endsnippet

$snippet enforcing_namespace_level_access_control_apply.sh syntax="bash"
$ kubectl apply -f @samples/bookinfo/platform/kube/rbac/namespace-policy.yaml@
$endsnippet
```

Snippets are written to a file in the working directory of the test with the extension
`.snippets.txt`. The name of the file (minus the extension) is specified when creating
the `Builder`.

For example, `istioio.NewBuilder("path__to__my_istioio_content")` would
generate the file `path__to__my_istioio_content.snippets.txt`.

By convention, the file name should generally indicate the path to the content on `istio.io`.
This helps to simplify collection and processing of these files later on.

For example, the snippets for the page
`Tasks->Secuity->Mutual TLS Migration` might be stored in
`tasks__security__mutual_tls_migration.snippets.txt`.

## Test Authoring Overview

To write an `istio.io` follow these steps:

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

1. To create a test, you use `istioio.Builder` to build a series of steps that will
be run as part of the resulting test function:

```golang
func TestCombinedMethods(t *testing.T) {
    framework.
        NewTest(t).
        Run(istioio.NewBuilder("tasks__security__my_task").
            // Run a script and create a snippet.
            Add(istioio.Command{
                Input:         istioio.Path("myscript.sh"),
                WorkDir:       env.IstioSrc,
                CreateSnippet: true,
            },
            // Wait for all pods in namespace foo to start.
            istioio.MultiPodWait("foo"),
            // Run another script and verify the results.
            istioio.Command{
                Input:         istioio.Path("myotherscript.sh"),
                WorkDir:       env.IstioSrc,
                CreateSnippet: true,
                Verify:        istioio.TokenVerifier(istioio.Path("myotherscript_verify.txt`),
            }).Build())
}
```

## Builder

The `istioio.NewBuilder` returns a `istioio.Builder` that is used to build an Istio
test run function and has the following methods:

- `Add`: adds a step to the test.
- `Defer`: provides a step to be run after the test completes.
- `Build`: builds an Istio test run function.

## Selecting Input

Many test steps require an `Input` which they obtain from an
`istioio.InputSelector`:

```golang
type Input interface {
    InputSelector
    Name() string
    ReadAll() (string, error)
}

type InputSelector interface {
    SelectInput(Context) Input
}
```

Some common `InputSelector` implementations include:

- `istioio.Inline`: allows you to inline the content for the `Input` directly in the code.
- `istioio.Path`: reads in a file from the specified path.
- `istioio.BookInfo`: is like `istioio.Path` except that the value is assumed to be
relative to the BookInfo source directory (`$GOPATH/src/istio.io/istio/samples/bookinfo/platform/kube/`).

An `InputSelector` provides an `istioio.Context` at runtime, which it can use to
dynamically choose an `Input`. For example, we could choose a different file depending on
whether or not the test is running on Minikube:

```golang
istioio.InputSelectorFunc(func(ctx istioio.Context) Input {
    if ctx.Env.Settings().Minikube {
        return istioio.Path("scripts/curl-httpbin-tls-gateway-minikube.sh")
    }
    return istioio.Path("scripts/curl-httpbin-tls-gateway-gke.sh")
})
```

The library also provides a utility that helps simplify this particular use case:

```golang
istioio.IfMinikube{
    Then: istioio.Path("scripts/curl-httpbin-tls-gateway-minikube.sh")
    Else: istioio.Path("scripts/curl-httpbin-tls-gateway-gke.sh")
}
```

## Run Shell Commands

You can create a test step that will run a shell command with `istioio.Command`:

```golang
istioio.Command{
    Input:         istioio.Path("myscript.sh"),
    WorkDir:       env.IstioSrc,
    CreateSnippet: true,
    Verify:        istioio.TokenVerifier(istioio.Path("myscript_verify.txt`),
}
```

This will read the given `Input`, verify that the output matches the given file, and
generate a snippet for the command.

See the `istioio.Command` API for additional configuration options.

## YAML Snippets

While a YAML file is applied via `istioio.Command`, you may want to create
a snippet that contains a single resource of an applied YAML:

```golang
istioio.YamlResource("my-resource-name", istioio.BookInfo("multi-resource-doc.yaml")),
```

The `istioio.YamlResource` function creates an `InputSelector` that parses the  given
`Input` and returns only the content for the given named resource.

## Waiting for Pods to Start

You can create a test step that waits for one or more pods to start before continuing.
For example, to wait for all pods in the "foo"  namespace, you can do the following:

```golang
istioio.MultiPodWait("foo"),
```

## Running the Tests: Make

You can execute all istio.io tests using make.

```bash
export KUBECONFIG=~/.kube/config
make test.integration.istioio.kube.presubmit
```

## Running Tests: go test

You can execute individual tests using Go test as shown below.

```bash
go test ./tests/integration/istioio/... -p 1  --istio.test.env kube \
    --istio.test.ci --istio.test.work_dir <my_dir>
```

The value of `my_dir` will be the parent directory for your test output. Within
`my_dir`, each test `Main` will create a directory containing a subdirectory for
each test method. Each test method directory will contain a `snippet.txt` that
was generated for that particular test.

Make sure to have the `HUB` and `TAG` environment variables set to the location of
your Istio Docker images.

You can find the complete list of arguments on [the test framework wiki page](https://github.com/istio/istio/wiki/Istio-Test-Framework).
