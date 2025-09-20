package iptablescheck_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"istio.io/istio/tools/linters/iptablescheck"
)

func TestAnalyzer(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, iptablescheck.Analyzer, "a")
}