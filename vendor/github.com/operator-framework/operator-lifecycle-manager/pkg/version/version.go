package version

import "fmt"

// OLMVersion indicates what version of OLM the binary belongs to
var OLMVersion string

// GitCommit indicates which git commit the binary was built from
var GitCommit string

// String returns a pretty string concatenation of OLMVersion and GitCommit
func String() string {
	return fmt.Sprintf("OLM version: %s\ngit commit: %s\n", OLMVersion, GitCommit)
}
