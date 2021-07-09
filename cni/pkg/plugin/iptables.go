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
	"github.com/spf13/viper"

	"istio.io/istio/tools/istio-iptables/pkg/cmd"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	"istio.io/pkg/env"
)

var (
	dryRunFilePath = env.RegisterStringVar("DRY_RUN_FILE_PATH", "", "")
)

type iptables struct{}

func newIPTables() InterceptRuleMgr {
	return &iptables{}
}

// Program defines a method which programs iptables based on the parameters
// provided in Redirect.
func (ipt *iptables) Program(netns string, rdrct *Redirect) error {
	viper.Set(constants.CNIMode, true)
	viper.Set(constants.NetworkNamespace, netns)
	viper.Set(constants.EnvoyPort, rdrct.targetPort)
	viper.Set(constants.ProxyUID, rdrct.noRedirectUID)
	viper.Set(constants.InboundInterceptionMode, rdrct.redirectMode)
	viper.Set(constants.ServiceCidr, rdrct.includeIPCidrs)
	viper.Set(constants.InboundPorts, rdrct.includePorts)
	viper.Set(constants.LocalExcludePorts, rdrct.excludeInboundPorts)
	viper.Set(constants.LocalOutboundPortsExclude, rdrct.excludeOutboundPorts)
	viper.Set(constants.ServiceExcludeCidr, rdrct.excludeIPCidrs)
	viper.Set(constants.KubeVirtInterfaces, rdrct.kubevirtInterfaces)
	if rdrct.dnsRedirect {
		viper.Set(constants.RedirectDNS, true)
		viper.Set(constants.CaptureAllDNS, true)
	}
	iptablesCmd := cmd.GetCommand()
	if err := iptablesCmd.Execute(); err != nil {
		return err
	}
	return nil
}
