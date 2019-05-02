package envtest

import (
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
)

// NewlineReporter is Reporter that Prints a newline after the default Reporter output so that the results
// are correctly parsed by test automation.
// See issue https://github.com/jstemmer/go-junit-report/issues/31
// It's re-exported here to avoid compatibility breakage/mass rewrites.
type NewlineReporter = printer.NewlineReporter
