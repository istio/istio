# testlinter

testlinter applies different linter rules to test files according to their categories, based on file paths and names.
It is run as part of the Istio pre-submit linter check. Whitelisting allows rule breaking exceptions, and temporarily
opt-out.

testlinter is based on [Checker](../README.md), and this package provides the [custom rules](rules) implementation.

## End To End Tests

All "_test.go" files in a "e2e" directory hierarchy are considered as end to end tests.

Example:

```bash
/istio/tests/e2e/tests/simple/simple_test.go
```

### End-to-end test rules

1. All skipped tests must be associated with an github issue.

1. All tests should be skipped if testing.short() is true.  This makes it easier to filter out long running tests
   using “go test -short ./…”.. Example (from [golang testing doc](https://golang.org/pkg/testing/)):

    ```go
    func TestTimeConsuming(t *testing.T) {
        if testing.Short() {
               t.Skip("skipping test in short mode.")
        }
        ...
    }
    ```

## Integration Tests

All "_test.go" files in an "integration" directory hierarchy, or with "_integ_test.go" suffix are considered as
integration tests.

Example:

```plain
/istio/tests/integration/tests/simple/simple_tests.go
/istio/mixer/simple_integ_tests.go

```

### Integration Test Rules

1. All skipped tests must be associated with an github issue.
1. All tests should be skipped if testing.short() is true. (TBD)

## Unit Tests

All "_test.go" files that are not integration tests and end to end tests are considered as unit tests. Most tests
are supposed to be in this category.

Example:

```plain
/istio/mixer/simple_tests.go

```

### Unit Test Rules

1. All skipped tests must be associated with an github issue.
1. Must not fork a new process.
1. Must not sleep, as unit tests are supposed to finish quickly. (Open to debate)

## Whitelist

If, for some reason, you want to disable lint rule for a file, you can add the file path and rule ID in
[whitelist.go](whitelist.go). Rule ID is the name of that rule file without `.go` extension.
You could also specify file path in regex.

If you want to disable all rules for a file path, you can specify `*` as the ID.

Example:

```go
var Whitelist = map[string][]string{
    "/istio/mixer/pkg/*": {"skip_issue", "short_skip"},
    "/istio/pilot/pkg/simply_test.go": {"*"},
}
```

## Running testlinter

There are two ways to run this linter.

```bash
go install
testlinter <target path>
```

```bash
go install
gometalinter --config=gometalinter.json <target path>
```
