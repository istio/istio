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

	useNftables := args.NativeNftables

	// To support safe migration from iptables to nftables backend, detect if the host already has any iptable artifacts.
	if useNftables {
		log.Info("Native nftables is configured, checking for iptables artifacts...")
		iptablesDetected, err := detectIptablesArtifacts(args.EnableIPv6)

		if iptablesDetected {
			// Override nftables configuration and continue with iptables backend
			log.Warnf("iptables artifacts detected (IPsets exist). " +
				"Overriding nftables configuration and continuing with iptables backend. " +
				"To complete migration to nftables, reboot the node.")
			useNftables = false
		} else {
			if err != nil {
				// Error while detecting the artifacts. Default to nftables (as requested) for a fail-safe behavior.
				log.Warnf("iptables artifacts could not be detected (%v). "+
					"Proceeding with nftables backend as configured.", err)
			} else {
				log.Info("iptables artifacts not found, proceeding with nftables backend")
			}
		}
	}

	log.Infof("creating host addressSet manager in the node netns")
	setManager, err := createHostNetworkAddrSetManager(useNftables, hostCfg.EnableIPv6)
	if err != nil {
		return nil, fmt.Errorf("error initializing host addressSet manager: %w", err)
	}

	podNsMap := newPodNetnsCache(openNetnsInRoot(pconstants.HostMountsPath))
	ztunnelServer, err := newZtunnelServer(args.ServerSocket, podNsMap, defaultZTunnelKeepAliveCheckInterval)
	if err != nil {
		return nil, fmt.Errorf("error initializing the ztunnel server: %w", err)
	}

	hostTrafficManager, podTrafficManager, err := trafficmanager.NewTrafficRuleManager(&trafficmanager.TrafficRuleManagerConfig{
		NativeNftables: useNftables,
		HostConfig:     hostCfg,
		PodConfig:      podCfg,
		HostDeps:       realDependenciesHost(args.ForceIptablesBinary),
		PodDeps:        realDependenciesInpod(UseScopedIptablesLegacyLocking, args.ForceIptablesBinary),
		NlDeps:         iptables.RealNlDeps(),
	})
	if err != nil {
		return nil, fmt.Errorf("error creating traffic managers: %w", err)
	}

	if !useNftables {
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

func realDependenciesHost(forceIptablesBinary string) *dep.RealDependencies {
	return &dep.RealDependencies{
		UsePodScopedXtablesLock: false,
		NetworkNamespace:        "",
		ForceIptablesBinary:     forceIptablesBinary,
	}
}

func realDependenciesInpod(useScopedLocks bool, forceIptablesBinary string) *dep.RealDependencies {
	return &dep.RealDependencies{
		UsePodScopedXtablesLock: useScopedLocks,
		NetworkNamespace:        "",
		ForceIptablesBinary:     forceIptablesBinary,
	}
}
