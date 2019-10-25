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
            Add(istioio.Script{
                Input:         istioio.Path("myscript.sh"),
                WorkDir:       env.IstioSrc,
            },
            istioio.MultiPodWait("foo"),
            istioio.Script{
                Input:         istioio.Path("myotherscript.sh"),
                WorkDir:       env.IstioSrc,
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

## Running Shell Commands

You can create a test step that will run a shell script and automatically generate
snippets with `istioio.Script`:

```golang
istioio.Script{
    Input:   istioio.Path("myscript.sh"),
    WorkDir: env.IstioSrc,
}
```

You can embed snippets directly in the input. Snippets must be surrounded by the
tokens `# $snippet` and `# $endsnippet`, each of which must be placed on their own
line and must be at the start of that line. For example:

```bash
# $snippet dostuff.sh syntax="bash"
$ kubectl apply -f @samples/bookinfo/platform/kube/rbac/namespace-policy.yaml@
# $endsnippet
```

This will run the command
`kubectl apply -f samples/bookinfo/platform/kube/rbac/namespace-policy.yaml` and
also generate the snippet.

Snippets that reference files in the
[Istio Github repository](`https://github.com/istio/istio`), as shown above, should
surround those links with `@`. The reference will be converted into an actual link
when rendering the page on `istio.io`.

The `$snippet` line supports a number of fields separated by whitespace. The first
field is the name of the snippet and is required. After the name, a number of
arguments in the form of `<key>="<value>"` may be provided. The following arguments
are supported:

- **syntax**: Sets the syntax for the command text. This is used to properly
highlight the output on istio.io.
- **outputis**: Sets the syntax for the output of the last command in the snippet.
If set, and the snippet contains expected output, verification will automatically
be performed against this expected output. By default, the commands and
output will be merged into a single snippet. This can be overridden with
`outputsnippet`.
- **outputsnippet**: A boolean value, which if "true" indicates that the output for
the last command of the snippet should appear in a separate snippet. The name of
the generated snippet will append "_output.txt" to the name of the current snippet.

You can indicate that a snippet should display output via `outputis`. Additionally,
you can verify the output of the last command in the snippet against expected
results with the `# $snippetoutput` annotation.

Example with verified output:

```bash
# $snippet mysnippet syntax="bash" outputis="text" outputsnippet="true"
$ kubectl apply -f @samples/bookinfo/platform/kube/rbac/namespace-policy.yaml@
# $snippetoutput verifier="token"
servicerole.rbac.istio.io/service-viewer created
servicerolebinding.rbac.istio.io/bind-service-viewer created
# $endsnippet
```

This will run the command
`kubectl apply -f samples/bookinfo/platform/kube/rbac/namespace-policy.yaml` and
will use the token-based verifier to verify that the output matches:

```text
servicerole.rbac.istio.io/service-viewer created
servicerolebinding.rbac.istio.io/bind-service-viewer created
```

Since `outputsnippet="true"`, it will then create a separate snippet for the actual output of
the last command:

```bash
# $snippet enforcing_namespace_level_access_control_apply.sh_output.txt syntax="text"
servicerole.rbac.istio.io/service-viewer created
servicerolebinding.rbac.istio.io/bind-service-viewer created
# $endsnippet
```

The following verifiers are supported:

- **token**: the default verifier, if not specified. Performs a token-based comparison of expected
and actual output. The syntax supports the wildcard `?` character to skip comparison
for a given token.
- **contains**: verifies that the output contains the given string.
- **notContains**: verifies that the output does not contain the given string.

See the `istioio.Script` API for additional configuration options.

## YAML Snippets

You can generate snippets for individual resources within a given YAML file:

```golang
istioio.YamlResources{
    BaseName:      "mysnippet",
    Input:         istioio.BookInfo("rbac/namespace-policy.yaml"),
    ResourceNames: []string{"service-viewer", "bind-service-viewer"},
}
```

The step `istioio.YamlResources` parses the  given `Input` and generates a
snippet for each named resource. You can configure the prefix of the generated
snippet names with the `BaseName` parameter. The snippet for the `service-viewer`
resource, for example, would be named `mysnippet_service-viewer.txt`. If not
specified, `BaseName` will be derived from the `Input` name.

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
