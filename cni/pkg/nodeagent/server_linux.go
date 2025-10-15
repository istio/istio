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

	set "istio.io/istio/cni/pkg/addressset"
	"istio.io/istio/cni/pkg/config"
	pconstants "istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/cni/pkg/trafficmanager"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/kube"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func initMeshDataplane(client kube.Client, args AmbientArgs) (*meshDataplane, error) {
	// Linux specific startup operations
	hostCfg := &config.AmbientConfig{
		RedirectDNS:            args.DNSCapture,
		EnableIPv6:             args.EnableIPv6,
		HostProbeSNATAddress:   HostProbeSNATIP,
		HostProbeV6SNATAddress: HostProbeSNATIPV6,
	}

	podCfg := &config.AmbientConfig{
		RedirectDNS:            args.DNSCapture,
		EnableIPv6:             args.EnableIPv6,
		HostProbeSNATAddress:   HostProbeSNATIP,
		HostProbeV6SNATAddress: HostProbeSNATIPV6,
		Reconcile:              args.ReconcilePodRulesOnStartup,
	}

	log.Infof("creating host addressSet manager in the node netns")
	setManager, err := createHostNetworkAddrSetManager(args.NativeNftables, hostCfg.EnableIPv6)
	if err != nil {
		return nil, fmt.Errorf("error initializing host addressSet manager: %w", err)
	}

	podNsMap := newPodNetnsCache(openNetnsInRoot(pconstants.HostMountsPath))
	ztunnelServer, err := newZtunnelServer(args.ServerSocket, podNsMap, defaultZTunnelKeepAliveCheckInterval)
	if err != nil {
		return nil, fmt.Errorf("error initializing the ztunnel server: %w", err)
	}

	hostTrafficManager, podTrafficManager, err := trafficmanager.NewTrafficRuleManager(&trafficmanager.TrafficRuleManagerConfig{
		NativeNftables: args.NativeNftables,
		HostConfig:     hostCfg,
		PodConfig:      podCfg,
		HostDeps:       realDependenciesHost(args.ForceIpTablesVersion),
		PodDeps:        realDependenciesInpod(UseScopedIptablesLegacyLocking, args.ForceIpTablesVersion),
		NlDeps:         iptables.RealNlDeps(),
	})
	if err != nil {
		return nil, fmt.Errorf("error creating traffic managers: %w", err)
	}

	if !args.NativeNftables {
		// The nftables implementation will automatically flush any pre-existing chains when programming
		// the rules, so we skip the DeleteHostRules for nftables backend.
		hostTrafficManager.DeleteHostRules()
	}

	// Create hostprobe rules now, in the host netns
	if err := hostTrafficManager.CreateHostRulesForHealthChecks(); err != nil {
		return nil, fmt.Errorf("error initializing the host rules for health checks: %w", err)
	}

	podNetns, err := NewPodNetnsProcFinder(os.DirFS(filepath.Join(pconstants.HostMountsPath, "proc")))
	if err != nil {
		return nil, err
	}
	netServer := newNetServer(ztunnelServer, podNsMap, podTrafficManager, podNetns)

	return &meshDataplane{
		kubeClient:         client.Kube(),
		netServer:          netServer,
		hostTrafficManager: hostTrafficManager,
		hostAddrSet:        setManager,
	}, nil
}

// createHostNetworkAddrSetManager creates a host network addressSet manager. This is designed to be called from the host netns.
// Note that if the set already exists by name, Create will not return an error.
//
// We will unconditionally flush our set before use here, so it shouldn't matter.
func createHostNetworkAddrSetManager(useNftables bool, isV6 bool) (set.AddressSetManager, error) {
	var setManager set.AddressSetManager
	runErr := util.RunAsHost(func() error {
		var err error
		setManager, err = set.New(useNftables, isV6)
		if err != nil {
			return err
		}
		setManager.Flush()
		return nil
	})
	return setManager, runErr
}

func realDependenciesHost(forceIptablesVersion string) *dep.RealDependencies {
	return &dep.RealDependencies{
		UsePodScopedXtablesLock: false,
		NetworkNamespace:        "",
		ForceIpTablesVersion:    forceIptablesVersion,
	}
}

func realDependenciesInpod(useScopedLocks bool, forceIptablesVersion string) *dep.RealDependencies {
	return &dep.RealDependencies{
		UsePodScopedXtablesLock: useScopedLocks,
		NetworkNamespace:        "",
		ForceIpTablesVersion:    forceIptablesVersion,
	}
}
