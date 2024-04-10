// Copyright Istio Authors
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
	"istio.io/istio/tools/istio-clean-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/builder"
	common "istio.io/istio/tools/istio-iptables/pkg/capture"
	types "istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func NewDependencies(cfg *config.Config) dep.Dependencies {
	if cfg.DryRun {
		return &dep.DependenciesStub{}
	}
	return &dep.RealDependencies{}
}

type IptablesCleaner struct {
	ext   dep.Dependencies
	cfg   *config.Config
	iptV  *dep.IptablesVersion
	ipt6V *dep.IptablesVersion
}

func NewIptablesCleaner(cfg *config.Config, iptV, ipt6V *dep.IptablesVersion, ext dep.Dependencies) *IptablesCleaner {
	return &IptablesCleaner{
		ext:   ext,
		cfg:   cfg,
		iptV:  iptV,
		ipt6V: ipt6V,
	}
}

// TODO BML why are these not on the type?
func flushAndDeleteChains(ext dep.Dependencies, iptV *dep.IptablesVersion, table string, chains []string) {
	for _, chain := range chains {
		ext.RunQuietlyAndIgnore(constants.IPTables, iptV, nil, "-t", table, "-F", chain)
		ext.RunQuietlyAndIgnore(constants.IPTables, iptV, nil, "-t", table, "-X", chain)
	}
}

func DeleteRule(ext dep.Dependencies, iptV *dep.IptablesVersion, table string, chain string, rulespec ...string) {
	args := append([]string{"-t", table, "-D", chain}, rulespec...)
	ext.RunQuietlyAndIgnore(constants.IPTables, iptV, nil, args...)
}

func removeOldChains(cfg *config.Config, ext dep.Dependencies, iptV *dep.IptablesVersion) {
	// Remove the old TCP rules
	for _, table := range []string{constants.NAT, constants.MANGLE} {
		ext.RunQuietlyAndIgnore(constants.IPTables, iptV, nil, "-t", table, "-D", constants.PREROUTING, "-p", constants.TCP, "-j", constants.ISTIOINBOUND)
	}
	ext.RunQuietlyAndIgnore(constants.IPTables, iptV, nil, "-t", constants.NAT, "-D", constants.OUTPUT, "-p", constants.TCP, "-j", constants.ISTIOOUTPUT)

	// Flush and delete the istio chains from NAT table.
	chains := []string{constants.ISTIOOUTPUT, constants.ISTIOINBOUND}
	flushAndDeleteChains(ext, iptV, constants.NAT, chains)
	// Flush and delete the istio chains from MANGLE table.
	chains = []string{constants.ISTIOINBOUND, constants.ISTIODIVERT, constants.ISTIOTPROXY}
	flushAndDeleteChains(ext, iptV, constants.MANGLE, chains)

	if cfg.InboundInterceptionMode == constants.TPROXY {
		DeleteRule(ext, iptV, constants.MANGLE, constants.PREROUTING,
			"-p", constants.TCP, "-m", "mark", "--mark", cfg.InboundTProxyMark, "-j", "CONNMARK", "--save-mark")
		DeleteRule(ext, iptV, constants.MANGLE, constants.OUTPUT,
			"-p", constants.TCP, "-m", "connmark", "--mark", cfg.InboundTProxyMark, "-j", "CONNMARK", "--restore-mark")
	}

	// Must be last, the others refer to it
	chains = []string{constants.ISTIOREDIRECT, constants.ISTIOINREDIRECT}
	flushAndDeleteChains(ext, iptV, constants.NAT, chains)
}

// cleanupDNSUDP removes any IPv4/v6 UDP rules.
// TODO BML drop `HandleDSNUDP` and friends, no real need to tread UDP rules specially
// or create unique abstractions for them
func cleanupDNSUDP(cfg *config.Config, ext dep.Dependencies, iptV, ipt6V *dep.IptablesVersion) {
	// Remove the old DNS UDP rules
	if cfg.RedirectDNS {
		ownerGroupsFilter := types.ParseInterceptFilter(cfg.OwnerGroupsInclude, cfg.OwnerGroupsExclude)

		common.HandleDNSUDP(common.DeleteOps, builder.NewIptablesRuleBuilder(nil), ext, iptV, ipt6V, cfg.ProxyUID, cfg.ProxyGID,
			cfg.DNSServersV4, cfg.DNSServersV6, cfg.CaptureAllDNS, ownerGroupsFilter)
	}
}

func (c *IptablesCleaner) Run() {
	defer func() {
		_ = c.ext.Run(constants.IPTablesSave, c.iptV, nil)
		_ = c.ext.Run(constants.IPTablesSave, c.ipt6V, nil)
	}()

	// clean v4/v6
	// Remove chains (run once per v4/v6)
	removeOldChains(c.cfg, c.ext, c.iptV)
	removeOldChains(c.cfg, c.ext, c.ipt6V)

	// Remove DNS UDP (runs for both v4 and v6 at the same time)
	cleanupDNSUDP(c.cfg, c.ext, c.iptV, c.ipt6V)
}
