# envvarlinter

envvarlinter ensures that non-test files don't use os.Getenv and os.LookupEnv and instead use the functions from pkg/env.

envvarlinter is based on [Checker](../README.md), and this package provides the [custom rules](rules) implementation.

# Whitelist

If, for some reason, you want to disable lint rule for a file, you can add the file path and rule ID in 
[whitelist.go](whitelist.go). Rule ID is the name of that rule file without `.go` extension.
You could also specify file path in regex.

If you want to disable all rules for a file path, you can specify `*` as the ID.

Example:
```base
var Whitelist = map[string][]string{
    "/istio/mixer/pkg/*": {"skip_issue", "short_skip"},
    "/istio/pilot/pkg/simply_test.go": {"*"},
}
```

# Running testlinter

There are two ways to run this linter.
```bash
go install 
testlinter <target path>
```
