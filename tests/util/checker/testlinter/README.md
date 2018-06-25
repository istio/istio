# testlinter

testlinter applies different linter rules to test files according to their categories, based on file paths and names. 
It is run as part of the Istio pre-submit linter check. Whitelisting allows rule breaking exceptions, and temporarily
opt-out.

testlinter is baed on [Checker](../README.md), and this package provides the [custom rules](rules) implementation.


## End To End Tests

All "_test.go" files in a "e2e" directory hierarchy are considered as end to end tests.

Example: 
```bash
/istio/tests/e2e/tests/simple/simple_test.go
```

#### Rules

1. All skipped tests must be associated with an github issue.
1. All tests should be skipped if testing.short() is true.  This makes it easier to filter out long running tests
   using “go test -short ./…”.. Example (from [golang testing doc](https://golang.org/pkg/testing/)):
```bash
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
```bash
/istio/tests/integration/tests/simple/simple_tests.go
/istio/mixer/simple_integ_tests.go

```

#### Rules

1. All skipped tests must be associated with an github issue.
1. All tests should be skipped if testing.short() is true. (TBD)


## Unit Tests

All "_test.go" files that are not integration tests and end to end tests are considered as unit tests. Most tests
are supposed to be in this category.

Example: 
```bash
/istio/mixer/simple_tests.go

```

#### Rules

1. All skipped tests must be associated with an github issue.
1. Must not fork a new process.
1. Must not sleep, as unit tests are supposed to finish quickly. (Open to debate)



# Whitelist
If, for some reason, you want to disable lint rules for tests, you can specify a whitelist for the tests by adding a whitelist comment. The comment should match this pattern, and ends with a period.
`// whitelist(url-to-GitHub-issue):[rule ID 1],[rule ID 2]...`
Rule ID is the name of that rule file without `.go` extension.
For example, you can skip no_sleep and skip_issue rules for test TestFunction:
```base
// whitelist(https://github.com/istio/istio/issues/6346):no_sleep,skip_issue.
func TestFunction(t *testing.T) {
	...
}
```
If you want to disable all rules for a test function, you can specify `*` as the ID.
For example:
```base
// whitelist(https://github.com/istio/istio/issues/6346):*.
func TestFunction(t *testing.T) {
	...
}
``` 
If you want to skip rules for all tests in a file, you can put the whitelist comment
into comment of TestMain.
For example, you can skip no_sleep and skip_issue rules for all tests:
```base
// whitelist(https://github.com/istio/istio/issues/6346):no_sleep,skip_issue.
func TestMain(t *testing.M) {
	...
}
```

# Running testlinter
There are two ways to run this linter.
```bash
go install 
testlinter <target path>
```

```bash
go install
gometalinter --config=gometalinter.json <target path>
```
