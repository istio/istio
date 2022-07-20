# Fuzzing Istio Code

The Istio (Go) code base is fuzzed using native Go fuzzing.
For general docs on how to fuzz in Go, see [Getting started with fuzzing](https://go.dev/doc/tutorial/fuzz).

## Writing a test

Generally, writing a fuzz test for Istio is the same as any other Go program.
However, because most of our fuzzing is based on complex structs rather than the primitives Go supports natively,
the `pkg/fuzz` package contains a number of helpers to fuzz.

Here is an example:

```go
// Define a new fuzzer. Must have Fuzz prefix
func FuzzBuildHTTP(f *testing.F) {
  fuzz.BaseCases(f) // Insert basic cases so a few trivial cases run in presubmit
  f.Fuzz(func(t *testing.T, data []byte) {
    fg := fuzz.New(t, data)
    // Setup a few structs for testing
    bundle := fuzz.Struct[trustdomain.Bundle](fg)
        // This one has a custom validator
    push := fuzz.Struct[*model.PushContext](fg, validatePush)
        // *model.Proxy, and other types, implement the fuzz.Validator interface and already validate some basics.
    node := fuzz.Struct[*model.Proxy](fg)
    option := fuzz.Struct[Option](fg)

    // Run our actual test code. In this case, we are just checking nothing crashes.
    // In other tests, explicit assertions may be helpful.
    policies := push.AuthzPolicies.ListAuthorizationPolicies(node.ConfigNamespace, node.Metadata.Labels)
    New(bundle, push, policies, option).BuildHTTP()
  })
}
```

## Running tests

Fuzz tests can be run using standard Go tooling:

```shell
go test ./path/to/pkg -v -run=^$ -fuzz=Fuzz
```

## CI testing

Go fuzzers are run as part of standard unit tests against known test cases (from `f.Add` (which `fuzz.BaseCases` calls), or `testdata`).
For continuous fuzzing, [`OSS-Fuzz`](https://github.com/google/oss-fuzz) continually builds and runs the fuzzers and reports any failures.
These results are private to the Istio Product Security WG until disclosed.
