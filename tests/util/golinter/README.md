# golinter

This golinter checks if our test files follow customized lint rules. It checks if a test file path
follows required patterns, and applies a list of linting rules to each file.

# Test file patterns
 1. Unit tests should be placed in a file with suffix "_test.go", and such file should stay out of 
 folder "e2e" and "integ". For example, "/foo/a_test.go".
 2. Integration tests could stay within folder "integ", and these tests should be placed in a file
 with suffix "_test.go" or "_integ_test.go". For example, "/integ/foo/bar/a_test.go", and 
 "/integ/foo/bar/b_integ_test.go". 
 
    Integration tests could stay out of folder "integ", in that case, these test should only stay in
     a file with suffix "_integ_test.go". For example, "/foo/bar/a_integ_test.go".
 3. End to end tests should only stay within folder "e2e", and should stay in a file with suffix
 "_test.go". For example, "../e2e/foo/bar/a_test.go".
  

# Lint rules.
[lint_rules_list.go](lint_rules_list.go) defines three lists of rules which apply to unit test file,
integration test file, and e2e test file. These lists are extensible. Each rule has an unique ID,
which is defined in [lint_rules.go](lint_rules.go).

1. SkipByIssue rule requires that a `t.Skip()` call in test function should contain url to a issue. 
This helps to keep tracking of the issue that causes a test to be skipped. 
For example, this is a valid call, 

    ```bash
    t.Skip("https://github.com/istio/istio/issues/6012")
    ```
    `t.SkipNow()` and `t.Skipf()` are not allowed.

2. NoSleep rule requires that `time.Sleep()` is not allowed. This rule is useful for checking unit
tests.

3. NoGoroutine rule requires that `go f(x, y, z)` is not allowed.

4. SkipByShort rule requires that a test function should have one of these pattern. This is needed
by large test such as an integration test or e2e test, so that we can specify a type of tests to run.
    ```bash
    Pattern 1
    func TestA(t *testing.T) {
      if !testing.Short() {
      ...
      }
    }
    
    Pattern 2
    func TestB(t *testing.T) {
      if testing.Short() {
        t.Skip("xxx")
      }
      ...
    }
    ``` 
# Whitelist
If, for some reason, you want to disable lint rule for a file, you can add file path and rule ID into 
[whitelist.go](whitelist.go). Rule ID is defined in [lint_rules.go](lint_rules.go).
multiple rule IDs are delimited by comma. You could also specify file path in regex.

If you don't want to apply any rule to a file path, you can choose `SkipAllRules`.

# Run golinter
There are two ways to run this linter.
```bash
go build main.go setup.go linter.go
./main <target path>
```

```bash
go install .
gometalinter --config=gometalinter.json <target path>
```