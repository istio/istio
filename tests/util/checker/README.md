# Checker

[Checker](checker.go) is an extensible go-file analyzer. It processes source codes in the following steps:
* Take a list of directory paths and recursively scan for go files
* Build abstract syntax trees for the relevant go files
* Apply Rule-based analysis
* Generate a report
 
The design is pluggable, as different applications can implement different [Rule](rule.go)s for different analytic
requirements.


### Check

The Check() function is the main entry point, which takes a list of file path (if empty, current directory is implied),
a RulesFactory, a [Whitelist](whitelist.go) , and a Report. 

The [RulesFactory](rule.go) and the [Whitelist](whitelist.go) are extension points which allow the caller (i.e.
application) to configure what [Rule](rule.go)s should apply to what files.

```bash
func Check(paths []string, factory RulesFactory, whitelist *Whitelist, report *Report) error
```


### Rule

Application implements the [Rule](rule.go) interface, and provides the actual logic to produce diagnostic information,
using an abstract syntax tree built from a go file. Each [Rule](rule.go) is expected to run one specific check
(potentially using information from external systems) against the given syntax tree, and multiple [Rule](rule.go)s can
be implemented for different checks against the same tree (i.e. go file). This allows custom semantic analysis.

```bash
type Rule interface {
	// GetID returns ID of the rule in string
	GetID() string
	// Check conditions using the given node and produce report if needed.
	Check(aNode ast.Node, fs *token.FileSet, lrp *Report)
}
```


### RulesFactory

Application implements the [RulesFactory](rule.go) interface to provide a mapping from file paths to their
corresponding rules. This allows fine grained control over what [Rule](rule.go)s should apply to what files. For
example, “_test.go” files can use a different set of rules from regular go files, or “mixer” files can have different
rules from “pilot” files.

```bash
type RulesFactory interface {
	// GetRules returns a list of rules used to check against the files.
	GetRules(absp string, info os.FileInfo) []Rule
}
```


### Whitelist

If, for some reason, you want to disable lint rule for a function, you can add a comment above the function. The comment should match this pattern, and ends with a period.
`// whitelist(url-to-GitHub-issue):[rule ID 1],[rule ID 2]...`
Rule ID is the name of that rule file without `.go` extension.
For example:
```base
// whitelist(https://github.com/istio/istio/issues/6346):no_sleep,skip_issue.
func exampleFunc() {
	...
}
```
If you want to disable all rules for function, you can specify `*` as the rule ID.
For example:
```base
// whitelist(https://github.com/istio/istio/issues/6346):*.
func exampleFunc() {
	...
}
``` 


### Report

[Report](report.go) is a utility used to accumulate rule diagnostic information for final consumption. When a
[Rule](rule.go) runs a check, it can add information to the [Report](report.go) using the AddItem() function to provide
file location information, and a diagnostic message.

After all checks are done, the Items() function can be used to retrieve the formatted text based report as a string
slice.

```bash
func (lr *Report) AddItem(pos token.Position, is string, msg string)

func (lt *Report) Items() []string
```


## Example Use Cases

* The [Checker](checker.go) can go through all Test* functions in _test.go files, and create a warning if a test is
  being skipped without having a github issue assigned (i.e. in the testing.T.skip() message). This allows us to
  properly track all skipped tests, and make sure they are restored when the underlying issues are fixed.

* The [Checker](checker.go) can go through all _test.go files and validate if the test is being run in CI. It can then
  find all the dead tests for developers to either remove, or to hook them back up in the automatic tests.
 
Information like these are valuable but are currently opaque to us (even codecov report cannot reveal these). The
extensible [Checker](checker.go) design opens up opportunities to add more code analysis, so we have better insight
into the Istio code base and maintain better code health.
