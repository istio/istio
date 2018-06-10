# Checker

The [Checker](checker.go) is an extensible go-file analyzer. It takes a list of directory paths, recursively scan for
go files, apply [Rule](rule.go)s to the files, and generate a text based report.

The design is pluggable, different applications can implement different [Rule](rule.go)s and define what
[Rule](rule.go)s should apply to different files.

## Rule

Application should implement the [Rule](rule.go) interface, and implement the actual logic
to produce diagnostic information, using an abstract syntax tree built from a go file. This allows custom
semantic analysis. Each [Rule](rule.go) is expected to run one specific check against the given syntax
tree, and multiple [Rule](rule.go) can be implemented for different checks against the same tree 
(i.e. go file).

```bash
type Rule interface {
	// GetID returns ID of the rule in string, ID is equal to the file name of that rule.
	GetID() string
	// Check verifies if aNode passes rule check. If verification fails lrp creates a report.
	Check(aNode ast.Node, fs *token.FileSet, lrp *Report)
}
```

## RulesFactory

Application should implement the [RulesFactory](rule.go) interface to improve the mapping between
a file and the applicable [Rule](rule.go)s. This allows a fine granularity control over what [Rule](rule.go)s
should apply to what files. For example, _test.go files can use a different set of rules from regular
go files.

```bash
type RulesFactory interface {
	// GetRules returns a list of rules used to check against the files.
	GetRules(absp string, info os.FileInfo) []Rule
}
```

## Whitelist

In addition to [RulesFactory](rule.go), application can provide a [Whitelist](whitelist.go) to whistlist
certain files from application some rules. This allows temporary rule disablement until the file is updated
to comply with the rules.

A [Whitelist](whitelist.go) object is backed by a map from string to a string slice, where the keys are file
paths, and the values are slice of rule IDs, as identified by return value of the GetID() method, in the 
[Rule](rule.go) interface.

```bash
func NewWhitelist(ruleWhitelist map[string][]string) *Whitelist {
	return &Whitelist{ruleWhitelist: ruleWhitelist}
}
```
## Report

[Report](report.go) is used to accumulate rule diagnostic information for final consumption. When a 
[Rule](rule.go) runs a check, it can add information to the [Report](report.go) using the AddItem function
to provide both the file location information, as well as a diagnostic text string. [report](report.go).

After the checks are done, the Items() function can be used to retrieve the formatted text based report,
as in a string slice.

```bash
func (lr *Report) AddItem(pos token.Pos, fs *token.FileSet, msg string)

func (lt *Report) Items() []string
```

## Check

The Check() function is the main entry point to Checker. It takes a list of file path (if empty, current 
directory is implied), a [RulesFactory](rule.go), a [Whitelist](whitelist.go) , and a [Report](report.go).

Both the [RulesFactory](rule.go), and the [Whitelist](whitelist.go) allows the caller to configure the
rules that should apply.

```bash
func Check(paths []string, factory RulesFactory, whitelist *Whitelist, report *Report) error {
```

# Example Use Cases

1. The [Checker](checker.go) can go through all Test* functions in _test.go files, and create a warning if
   a test is being skipped without having a github issue assigned (i.e. in the testing.T.skip() message)
   This allows us to properly track all skipped tests, and make sure they are restored when the underlying
   issue is fixed. 

1. The [Checker](checker.go) can go through all _test.go files and validate if the test is being run in CI.
   It can then find all the dead tests to either remove, or to hook them back up in the automatic tests.

Both these information are complementary to the coverage information we already obtained via codecov.