# golinter

golinter applies customized lint rules to our test files. Test files are categorized into
unit tests, integration tests, and end to end tests based on file locations and file names, and
different rules are applied accordingly.

golinter is baed on [Checker](../README.md), and this package provides the custom [Rule](../rule.go)s
implementation.


## End To End Tests

All "_test.go" files in a "e2e" directory hierarchy are considered as end to end tests.

Example: 
```bash
/istio/tests/e2e/tests/simple/simple_tests.go
```

#### Rules

1. TBD


## Integration Tests

All "_test.go" files in a "integration" directory hierarchy, or with "_integ_test.go" suffix are
considered as end to end tests.

Example: 
```bash
/istio/tests/integration/tests/simple/simple_tests.go
/istio/mixer/simple_integ_tests.go

```

#### Rules

1. TBD


## Unit Tests

All "_test.go" files that are not integration tests and end to end tests are considered as unit tests.

Example: 
```bash
/istio/mixer/simple_tests.go

```

#### Rules

1. TBD



# Whitelist
If, for some reason, you want to disable lint rule for a file, you can add the file path and rule ID in 
[whitelist.go](whitelist.go). Rule ID is the name of that rule file without `.go` extension.
You could also specify file path in regex.

If you don't want to disable all rule for a file path, you can specify `*` as the ID.

Example:
```base
var Whitelist = map[string][]string{
    "/istoi/mixer/pkg/*": {"skip_issue", "short_skip"},
    "/istoi/piloy/pkg/simply_test.go": {"*"},
}
```

# Running golinter
There are two ways to run this linter.
```bash
go install 
golinter <target path>
```

```bash
go install
gometalinter --config=gometalinter.json <target path>
```