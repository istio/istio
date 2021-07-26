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

// This is a sample chained plugin that supports multiple CNI versions. It
// parses prevResult according to the cniVersion
package plugin

import (
	"istio.io/istio/tools/istio-iptables/pkg/cmd"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

var dryRunFilePath = env.RegisterStringVar("DRY_RUN_FILE_PATH", "",
	"If provided, CNI will dry run iptables rule apply, and print the applied rules to the given file.")

type iptables struct{}

func newIPTables() InterceptRuleMgr {
	return &iptables{}
}

// Program defines a method which programs iptables based on the parameters
// provided in Redirect.
func (ipt *iptables) Program(podName, netns string, rdrct *Redirect) error {
	cfg := cmd.ConstructConfig(cmd.GetDefaultConfig())
	cfg.CNIMode = true
	cfg.NetworkNamespace = netns
	cfg.ProxyPort = rdrct.targetPort
	cfg.ProxyUID = rdrct.noRedirectUID
	cfg.InboundInterceptionMode = rdrct.redirectMode
	cfg.OutboundIPRangesInclude = rdrct.includeIPCidrs
	cfg.InboundPortsInclude = rdrct.includePorts
	cfg.InboundPortsExclude = rdrct.excludeInboundPorts
	cfg.OutboundPortsExclude = rdrct.excludeOutboundPorts
	cfg.OutboundIPRangesExclude = rdrct.excludeIPCidrs
	cfg.KubevirtInterfaces = rdrct.kubevirtInterfaces
	drf := dryRunFilePath.Get()
	cfg.DryRun = (drf != "")
	cfg.OutputPath = drf
	cfg.RedirectDNS = rdrct.dnsRedirect
	cfg.CaptureAllDNS = rdrct.dnsRedirect

	log.Infof("============= Start iptables configuration for %v =============", podName)
	defer log.Infof("============= End iptables configuration for %v =============", podName)
	if err := cmd.SetupIPTables(cfg); err != nil {
		return err
	}
	return nil
}
