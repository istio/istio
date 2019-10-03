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

	"istio.io/istio/tools/istio-iptables/pkg/constants"

	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func removeOldChains(ext dep.Dependencies, cmd dep.Cmd) {
	// Remove the old chains
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.NAT, "-D", constants.PREROUTING, "-p", constants.TCP, "-j", constants.ISTIOINBOUND)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.MANGLE, "-D", constants.PREROUTING, "-p", constants.TCP, "-j", constants.ISTIOINBOUND)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.NAT, "-D", constants.OUTPUT, "-p", constants.TCP, "-j", constants.ISTIOOUTPUT)
	// Flush and delete the istio chains.
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.NAT, "-F", constants.ISTIOOUTPUT)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.NAT, "-X", constants.ISTIOOUTPUT)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.NAT, "-F", constants.ISTIOINBOUND)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.NAT, "-X", constants.ISTIOINBOUND)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.MANGLE, "-F", constants.ISTIOINBOUND)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.MANGLE, "-X", constants.ISTIOINBOUND)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.MANGLE, "-F", constants.ISTIODIVERT)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.MANGLE, "-X", constants.ISTIODIVERT)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.MANGLE, "-F", constants.ISTIOTPROXY)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.MANGLE, "-X", constants.ISTIOTPROXY)
	// Must be last, the others refer to it
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.NAT, "-F", constants.ISTIOREDIRECT)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.NAT, "-X", constants.ISTIOREDIRECT)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.NAT, "-F", constants.ISTIOINREDIRECT)
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.NAT, "-X", constants.ISTIOINREDIRECT)
}

func run(args []string, flagSet *flag.FlagSet) {
	var dryRun bool
	flagSet.BoolVar(&dryRun, "dryRun", false, "Do not call any external dependencies like ipcmd")
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
		for _, cmd := range []dep.Cmd{dep.IPTABLESSAVE, dep.IP6TABLESSAVE} {
			ext.RunOrFail(cmd)
		}
	}()

	for _, cmd := range []dep.Cmd{dep.IPTABLES, dep.IP6TABLES} {
		removeOldChains(ext, cmd)
	}
}

func main() {
	run(os.Args[1:], flag.CommandLine)
}
