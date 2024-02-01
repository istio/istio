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
	"fmt"

	"github.com/containernetworking/plugins/pkg/ns"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/cmd"
	"istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

// getNs is a unit test override variable for interface create.
var getNs = ns.GetNS

// Program defines a method which programs iptables based on the parameters
// provided in Redirect.
func (ipt *iptables) Program(podName, netns string, rdrct *Redirect) error {
	cfg := config.DefaultConfig()
	cfg.CNIMode = true
	cfg.NetworkNamespace = netns
	cfg.ProxyPort = rdrct.targetPort
	cfg.ProxyUID = rdrct.noRedirectUID
	cfg.ProxyGID = rdrct.noRedirectGID
	cfg.InboundInterceptionMode = rdrct.redirectMode
	cfg.OutboundIPRangesInclude = rdrct.includeIPCidrs
	cfg.InboundPortsExclude = rdrct.excludeInboundPorts
	cfg.InboundPortsInclude = rdrct.includeInboundPorts
	cfg.ExcludeInterfaces = rdrct.excludeInterfaces
	cfg.OutboundPortsExclude = rdrct.excludeOutboundPorts
	cfg.OutboundPortsInclude = rdrct.includeOutboundPorts
	cfg.OutboundIPRangesExclude = rdrct.excludeIPCidrs
	cfg.KubeVirtInterfaces = rdrct.kubevirtInterfaces
	cfg.DryRun = dependencies.DryRunFilePath.Get() != ""
	cfg.RedirectDNS = rdrct.dnsRedirect
	cfg.CaptureAllDNS = rdrct.dnsRedirect
	cfg.DropInvalid = rdrct.invalidDrop
	cfg.DualStack = rdrct.dualStack
	cfg.FillConfigFromEnvironment()

	netNs, err := getNs(netns)
	if err != nil {
		err = fmt.Errorf("failed to open netns %q: %s", netns, err)
		return err
	}
	defer netNs.Close()

	return netNs.Do(func(_ ns.NetNS) error {
		log.Infof("============= Start iptables configuration for %v =============", podName)
		defer log.Infof("============= End iptables configuration for %v =============", podName)
		return cmd.ProgramIptables(cfg)
	})
}
