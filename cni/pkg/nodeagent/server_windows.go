//go:build windows
// +build windows

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
	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/pkg/kube"
)

func initMeshDataplane(client kube.Client, args AmbientArgs) (*meshDataplane, error) {
	podCfg := &iptables.IptablesConfig{
		RedirectDNS:            args.DNSCapture,
		EnableIPv6:             args.EnableIPv6,
		HostProbeSNATAddress:   HostProbeSNATIP,
		HostProbeV6SNATAddress: HostProbeSNATIPV6,
		Reconcile:              false, // Windows doesn't support reconcile
	}
	podNsMap := newPodNetnsCache(getNamespaceDetailsFromRoot())
	ztunnelServer, err := newZtunnelServer(args.ServerSocket, podNsMap)
	if err != nil {
		return nil, err
	}
	netServer := newNetServer(ztunnelServer, podNsMap, &iptables.WFPConfigurator{
		EndpointsFinder: podNsMap,
		Cfg:             podCfg,
	}, NewPodNetNsHNSFinder())
	return &meshDataplane{
		kubeClient: client.Kube(),
		netServer:  netServer,
	}, nil
}
