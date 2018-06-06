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
integration test file, and e2e test file. These lists are extensible. For example, to configure 
rules for unit tests check, you can add any rule creator into `UnitTestRules`, or remove
any rule creator out of `UnitTestRules`.  

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