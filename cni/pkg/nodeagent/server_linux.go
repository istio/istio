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

package nodeagent

import (
	"fmt"
	"os"
	"path/filepath"

	pconstants "istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/pkg/kube"
)

func initMeshDataplane(client kube.Client, args AmbientArgs) (*meshDataplane, error) {
	// Linux specific startup operations
	hostCfg := &iptables.IptablesConfig{
		RedirectDNS:            args.DNSCapture,
		EnableIPv6:             args.EnableIPv6,
		HostProbeSNATAddress:   HostProbeSNATIP,
		HostProbeV6SNATAddress: HostProbeSNATIPV6,
	}

	podCfg := &iptables.IptablesConfig{
		RedirectDNS:            args.DNSCapture,
		EnableIPv6:             args.EnableIPv6,
		HostProbeSNATAddress:   HostProbeSNATIP,
		HostProbeV6SNATAddress: HostProbeSNATIPV6,
		Reconcile:              args.ReconcilePodRulesOnStartup,
	}

	log.Debug("creating ipsets in the node netns")
	set, err := createHostsideProbeIpset(hostCfg.EnableIPv6)
	if err != nil {
		return nil, fmt.Errorf("error initializing hostside probe ipset: %w", err)
	}

	podNsMap := newPodNetnsCache(openNetnsInRoot(pconstants.HostMountsPath))
	ztunnelServer, err := newZtunnelServer(args.ServerSocket, podNsMap, defaultZTunnelKeepAliveCheckInterval)
	if err != nil {
		return nil, fmt.Errorf("error initializing the ztunnel server: %w", err)
	}

	hostIptables, podIptables, err := iptables.NewIptablesConfigurator(
		hostCfg,
		podCfg,
		realDependenciesHost(),
		realDependenciesInpod(UseScopedIptablesLegacyLocking),
		iptables.RealNlDeps(),
	)
	if err != nil {
		return nil, fmt.Errorf("error configuring iptables: %w", err)
	}

	// Create hostprobe rules now, in the host netns
	hostIptables.DeleteHostRules()

	if err := hostIptables.CreateHostRulesForHealthChecks(); err != nil {
		return nil, fmt.Errorf("error initializing the host rules for health checks: %w", err)
	}

	podNetns, err := NewPodNetnsProcFinder(os.DirFS(filepath.Join(pconstants.HostMountsPath, "proc")))
	if err != nil {
		return nil, err
	}
	netServer := newNetServer(ztunnelServer, podNsMap, podIptables, podNetns)

	return &meshDataplane{
		kubeClient:         client.Kube(),
		netServer:          netServer,
		hostIptables:       hostIptables,
		hostsideProbeIPSet: set,
	}, nil
}
