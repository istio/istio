package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"istio.io/istio/tools/linters/iptablescheck"
)

func main() {
	singlechecker.Main(iptablescheck.Analyzer)
}