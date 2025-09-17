// Binary iptablescheck is a standalone linter for detecting iptables comment flags
// that are incompatible with gVisor environments.
//
// This linter helps identify uses of iptables comment module (-m comment, --comment)
// which are not supported in gVisor and can cause runtime failures.
//
// See: https://github.com/istio/istio/issues/57678
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"istio.io/istio/tools/linters/iptablescheck"
)

func main() {
	singlechecker.Main(iptablescheck.Analyzer)
}