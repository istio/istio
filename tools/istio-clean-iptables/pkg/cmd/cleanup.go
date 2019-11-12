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

package cmd

import (
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func removeOldChains(ext dep.Dependencies, cmd string) {
	for _, table := range []string{constants.NAT, constants.MANGLE} {
		// Remove the old chains
		ext.RunQuietlyAndIgnore(cmd, "-t", table, "-D", constants.PREROUTING, "-p", constants.TCP, "-j", constants.ISTIOINBOUND)
	}
	ext.RunQuietlyAndIgnore(cmd, "-t", constants.NAT, "-D", constants.OUTPUT, "-p", constants.TCP, "-j", constants.ISTIOOUTPUT)
	// Flush and delete the istio chains.
	tableChainMap := map[string][]string{
		constants.NAT:    {constants.ISTIOOUTPUT, constants.ISTIOINBOUND},
		constants.MANGLE: {constants.ISTIOINBOUND, constants.ISTIODIVERT, constants.TPROXY},
	}
	for table, chains := range tableChainMap {
		for _, chain := range chains {
			ext.RunQuietlyAndIgnore(cmd, "-t", table, "-F", chain)
			ext.RunQuietlyAndIgnore(cmd, "-t", table, "-X", chain)
		}
	}
	// Must be last, the others refer to it
	for _, chain := range []string{constants.ISTIOREDIRECT, constants.ISTIOINREDIRECT} {
		ext.RunQuietlyAndIgnore(cmd, "-t", constants.NAT, "-F", chain)
		ext.RunQuietlyAndIgnore(cmd, "-t", constants.NAT, "-X", chain)
	}
}

func cleanup(dryRun bool) {
	var ext dep.Dependencies
	if dryRun {
		ext = &dep.StdoutStubDependencies{}
	} else {
		ext = &dep.RealDependencies{}
	}

	defer func() {
		for _, cmd := range []string{dep.IPTABLESSAVE, dep.IP6TABLESSAVE} {
			ext.RunOrFail(cmd)
		}
	}()

	for _, cmd := range []string{dep.IPTABLES, dep.IP6TABLES} {
		removeOldChains(ext, cmd)
	}
}
