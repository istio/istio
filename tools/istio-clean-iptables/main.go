// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"os"

	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func run(args []string, flagSet *flag.FlagSet) {
	var dryRun bool
	flagSet.BoolVar(&dryRun, "dryRun", false, "Do not call any external dependencies like iptables")
	err := flagSet.Parse(args)
	if err != nil {
		return
	}

	var ext dep.Dependencies
	if dryRun {
		ext = &dep.StdoutStubDependencies{}
	} else {
		ext = &dep.RealDependencies{}
	}

	defer func() {
		ext.RunOrFail(dep.IPTABLESSAVE)
		ext.RunOrFail(dep.IP6TABLESSAVE)
	}()

	// Remove the old chains, to generate new configs.
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-D", "OUTPUT", "-p", "tcp", "-j", "ISTIO_OUTPUT")
	// Flush and delete the istio chains.
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-F", "ISTIO_OUTPUT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-X", "ISTIO_OUTPUT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-F", "ISTIO_INBOUND")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-X", "ISTIO_INBOUND")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-F", "ISTIO_INBOUND")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-X", "ISTIO_INBOUND")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-F", "ISTIO_DIVERT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-X", "ISTIO_DIVERT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-F", "ISTIO_TPROXY")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-X", "ISTIO_TPROXY")
	// Must be last, the others refer to it
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-F", "ISTIO_REDIRECT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-X", "ISTIO_REDIRECT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-F", "ISTIO_IN_REDIRECT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-X", "ISTIO_IN_REDIRECT")
}

func main() {
	run(os.Args[1:], flag.CommandLine)
}
