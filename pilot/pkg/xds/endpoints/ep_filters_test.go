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

package endpoints_test

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"

	networking "istio.io/api/networking/v1alpha3"
	security "istio.io/api/security/v1beta1"
	"istio.io/api/label"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/xds/endpoints"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
        "istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
)

var networkFiltered = []networkFilterCase{
	{
		name:  "from_network1_cluster1a",
		proxy: makeProxy("network1", "cluster1a"),
		want: []xdstest.LocLbEpInfo{
			{
				LbEps: []xdstest.LbEpInfo{
					// 3 local endpoints on network1
					{Address: "10.0.0.1", Weight: 6},
					{Address: "10.0.0.2", Weight: 6},
					{Address: "10.0.0.3", Weight: 6},
					// 1 endpoint on network2, cluster2a
					{Address: "2.2.2.2", Weight: 6},
					// 2 endpoints on network2, cluster2b
					{Address: "2.2.2.20", Weight: 6},
					{Address: "2.2.2.21", Weight: 6},
					// 1 endpoint on network4 with no gateway (i.e. directly accessible)
					{Address: "40.0.0.1", Weight: 6},
				},
				Weight: 42,
			},
		},
		wantWorkloadMetadata: []string{
			";ns;example;;cluster1a",
			";ns;example;;cluster1a",
			";ns;example;;cluster1b",
			";ns;example;;cluster4",
			";;;;cluster2a",
			";;;;cluster2b",
			";;;;cluster2b",
		},
	},
	{
		name:  "from_network1_cluster1b",
		proxy: makeProxy("network1", "cluster1b"),
		want: []xdstest.LocLbEpInfo{
			{
				LbEps: []xdstest.LbEpInfo{
					// 3 local endpoints on network1
					{Address: "10.0.0.1", Weight: 6},
					{Address: "10.0.0.2", Weight: 6},
					{Address: "10.0.0.3", Weight: 6},
					// 1 endpoint on network2, cluster2a
					{Address: "2.2.2.2", Weight: 6},
					// 2 endpoints on network2, cluster2b
					{Address: "2.2.2.20", Weight: 6},
					{Address: "2.2.2.21", Weight: 6},
					// 1 endpoint on network4 with no gateway (i.e. directly accessible)
					{Address: "40.0.0.1", Weight: 6},
				},
				Weight: 42,
			},
		},
		wantWorkloadMetadata: []string{
			";ns;example;;cluster1a",
			";ns;example;;cluster1a",
			";ns;example;;cluster1b",
			";ns;example;;cluster4",
			";;;;cluster2a",
			";;;;cluster2b",
			";;;;cluster2b",
		},
	},
	{
		name:  "from_network2_cluster2a",
		proxy: makeProxy("network2", "cluster2a"),
		want: []xdstest.LocLbEpInfo{
			{
				LbEps: []xdstest.LbEpInfo{
					// 3 local endpoints in network2
					{Address: "20.0.0.1", Weight: 6},
					{Address: "20.0.0.2", Weight: 6},
					{Address: "20.0.0.3", Weight: 6},
					// 2 endpoint on network1 with weight aggregated at the gateway
					{Address: "1.1.1.1", Weight: 12},
					// 1 endpoint on network4 with no gateway (i.e. directly accessible)
					{Address: "40.0.0.1", Weight: 6},
				},
				Weight: 36,
			},
		},
		wantWorkloadMetadata: []string{
			";ns;example;;cluster2a",
			";ns;example;;cluster2b",
			";ns;example;;cluster2b",
			";ns;example;;cluster4",
			";;;;cluster1a",
		},
	},
	{
		name:  "from_network2_cluster2b",
		proxy: makeProxy("network2", "cluster2b"),
		want: []xdstest.LocLbEpInfo{
			{
				LbEps: []xdstest.LbEpInfo{
					// 3 local endpoints in network2
					{Address: "20.0.0.1", Weight: 6},
					{Address: "20.0.0.2", Weight: 6},
					{Address: "20.0.0.3", Weight: 6},
					// 2 endpoint on network1 with weight aggregated at the gateway
					{Address: "1.1.1.1", Weight: 12},
					// 1 endpoint on network4 with no gateway (i.e. directly accessible)
					{Address: "40.0.0.1", Weight: 6},
				},
				Weight: 36,
			},
		},
		wantWorkloadMetadata: []string{
			";ns;example;;cluster2a",
			";ns;example;;cluster2b",
			";ns;example;;cluster2b",
			";ns;example;;cluster4",
			";;;;cluster1a",
		},
	},
	{
		name:  "from_network3_cluster3",
		proxy: makeProxy("network3", "cluster3"),
		want: []xdstest.LocLbEpInfo{
			{
				LbEps: []xdstest.LbEpInfo{
					// 2 endpoint on network1 with weight aggregated at the gateway
					{Address: "1.1.1.1", Weight: 12},
					// 1 endpoint on network2, cluster2a
					{Address: "2.2.2.2", Weight: 6},
					// 2 endpoints on network2, cluster2b
					{Address: "2.2.2.20", Weight: 6},
					{Address: "2.2.2.21", Weight: 6},
					// 1 endpoint on network4 with no gateway (i.e. directly accessible)
					{Address: "40.0.0.1", Weight: 6},
				},
				Weight: 36,
			},
		},
		wantWorkloadMetadata: []string{
			";ns;example;;cluster4",
			";;;;cluster1a",
			";;;;cluster2a",
			";;;;cluster2b",
			";;;;cluster2b",
		},
	},
	{
		name:  "from_network4_cluster4",
		proxy: makeProxy("network4", "cluster4"),
		want: []xdstest.LocLbEpInfo{
			{
				LbEps: []xdstest.LbEpInfo{
					// 1 local endpoint on network4
					{Address: "40.0.0.1", Weight: 6},
					// 2 endpoint on network1 with weight aggregated at the gateway
					{Address: "1.1.1.1", Weight: 12},
					// 1 endpoint on network2, cluster2a
					{Address: "2.2.2.2", Weight: 6},
					// 2 endpoints on network2, cluster2b
					{Address: "2.2.2.20", Weight: 6},
					{Address: "2.2.2.21", Weight: 6},
				},
				Weight: 36,
			},
		},
		wantWorkloadMetadata: []string{
			";ns;example;;cluster4",
			";;;;cluster1a",
			";;;;cluster2a",
			";;;;cluster2b",
			";;;;cluster2b",
		},
	},
}

var mtlsCases = map[string]map[string]struct {
	Config         config.Config
	Configs        []config.Config
	IsMtlsDisabled bool
	SubsetName     string
}{
	gvk.PeerAuthentication.String(): {
		"mtls-off-ineffective": {
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.PeerAuthentication,
					Name:             "mtls-partial",
					Namespace:        "istio-system",
				},
				Spec: &security.PeerAuthentication{
					Selector: &v1beta1.WorkloadSelector{
						// shouldn't affect our test workload
						MatchLabels: map[string]string{"app": "b"},
					},
					Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_DISABLE},
				},
			},
			IsMtlsDisabled: false,
		},
		"mtls-on-strict": {
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.PeerAuthentication,
					Name:             "mtls-on",
					Namespace:        "istio-system",
				},
				Spec: &security.PeerAuthentication{
					Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_STRICT},
				},
			},
			IsMtlsDisabled: false,
		},
		"mtls-off-global": {
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.PeerAuthentication,
					Name:             "mtls-off",
					Namespace:        "istio-system",
				},
				Spec: &security.PeerAuthentication{
					Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_DISABLE},
				},
			},
			IsMtlsDisabled: true,
		},
		"mtls-off-namespace": {
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.PeerAuthentication,
					Name:             "mtls-off",
					Namespace:        "ns",
				},
				Spec: &security.PeerAuthentication{
					Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_DISABLE},
				},
			},
			IsMtlsDisabled: true,
		},
		"mtls-off-workload": {
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.PeerAuthentication,
					Name:             "mtls-off",
					Namespace:        "ns",
				},
				Spec: &security.PeerAuthentication{
					Selector: &v1beta1.WorkloadSelector{
						MatchLabels: map[string]string{"app": "example"},
					},
					Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_DISABLE},
				},
			},
			IsMtlsDisabled: true,
		},
		"mtls-off-port": {
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.PeerAuthentication,
					Name:             "mtls-off",
					Namespace:        "ns",
				},
				Spec: &security.PeerAuthentication{
					Selector: &v1beta1.WorkloadSelector{
						MatchLabels: map[string]string{"app": "example"},
					},
					PortLevelMtls: map[uint32]*security.PeerAuthentication_MutualTLS{
						8080: {Mode: security.PeerAuthentication_MutualTLS_DISABLE},
					},
				},
			},
			IsMtlsDisabled: true,
		},
	},
	gvk.DestinationRule.String(): {
		"mtls-on-override-pa": {
			Configs: []config.Config{
				{
					Meta: config.Meta{
						GroupVersionKind: gvk.PeerAuthentication,
						Name:             "mtls-off",
						Namespace:        "ns",
					},
					Spec: &security.PeerAuthentication{
						Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_DISABLE},
					},
				},
				{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "mtls-on",
						Namespace:        "ns",
					},
					Spec: &networking.DestinationRule{
						Host: "example.ns.svc.cluster.local",
						TrafficPolicy: &networking.TrafficPolicy{
							Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
						},
					},
				},
			},
			IsMtlsDisabled: false,
		},
		"mtls-off-innefective": {
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "mtls-off",
					Namespace:        "ns",
				},
				Spec: &networking.DestinationRule{
					Host: "other.ns.svc.cluster.local",
					TrafficPolicy: &networking.TrafficPolicy{
						Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_DISABLE},
					},
				},
			},
			IsMtlsDisabled: false,
		},
		"mtls-on-destination-level": {
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "mtls-on",
					Namespace:        "ns",
				},
				Spec: &networking.DestinationRule{
					Host: "example.ns.svc.cluster.local",
					TrafficPolicy: &networking.TrafficPolicy{
						Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
					},
				},
			},
			IsMtlsDisabled: false,
		},
		"mtls-on-port-level": {
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "mtls-on",
					Namespace:        "ns",
				},
				Spec: &networking.DestinationRule{
					Host: "example.ns.svc.cluster.local",
					TrafficPolicy: &networking.TrafficPolicy{
						PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{{
							Port: &networking.PortSelector{Number: 80},
							Tls:  &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
						}},
					},
				},
			},
			IsMtlsDisabled: false,
		},
		"mtls-off-destination-level": {
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "mtls-off",
					Namespace:        "ns",
				},
				Spec: &networking.DestinationRule{
					Host: "example.ns.svc.cluster.local",
					TrafficPolicy: &networking.TrafficPolicy{
						Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_DISABLE},
					},
				},
			},
			IsMtlsDisabled: true,
		},
		"mtls-off-port-level": {
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "mtls-off",
					Namespace:        "ns",
				},
				Spec: &networking.DestinationRule{
					Host: "example.ns.svc.cluster.local",
					TrafficPolicy: &networking.TrafficPolicy{
						PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{{
							Port: &networking.PortSelector{Number: 80},
							Tls:  &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_DISABLE},
						}},
					},
				},
			},
			IsMtlsDisabled: true,
		},
		"mtls-off-subset-level": {
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "mtls-off",
					Namespace:        "ns",
				},
				Spec: &networking.DestinationRule{
					Host: "example.ns.svc.cluster.local",
					TrafficPolicy: &networking.TrafficPolicy{
						// should be overridden by subset
						Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
					},
					Subsets: []*networking.Subset{{
						Name:   "disable-tls",
						Labels: map[string]string{"app": "example"},
						TrafficPolicy: &networking.TrafficPolicy{
							Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_DISABLE},
						},
					}},
				},
			},
			IsMtlsDisabled: true,
			SubsetName:     "disable-tls",
		},
		"mtls-on-subset-level": {
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "mtls-on",
					Namespace:        "ns",
				},
				Spec: &networking.DestinationRule{
					Host: "example.ns.svc.cluster.local",
					TrafficPolicy: &networking.TrafficPolicy{
						// should be overridden by subset
						Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_DISABLE},
					},
					Subsets: []*networking.Subset{{
						Name:   "enable-tls",
						Labels: map[string]string{"app": "example"},
						TrafficPolicy: &networking.TrafficPolicy{
							Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL},
						},
					}},
				},
			},
			IsMtlsDisabled: false,
			SubsetName:     "enable-tls",
		},
	},
}

func TestEndpointsByNetworkFilter(t *testing.T) {
	test.SetForTest(t, &features.MultiNetworkGatewayAPI, true)
	test.SetForTest(t, &features.PreferHBONESend, true)
	env := environment(t)
	env.Env().InitNetworksManager(env.Discovery)
	// The tests below are calling the endpoints filter from each one of the
	// networks and examines the returned filtered endpoints
	runNetworkFilterTest(t, env, networkFiltered, "")
}

func TestEndpointsByNetworkFilter_WithConfig(t *testing.T) {
	test.SetForTest(t, &features.MultiNetworkGatewayAPI, true)
	test.SetForTest(t, &features.PreferHBONESend, true)
	noCrossNetwork := []networkFilterCase{
		{
			name:  "from_network1_cluster1a",
			proxy: makeProxy("network1", "cluster1a"),
			want: []xdstest.LocLbEpInfo{
				{
					LbEps: []xdstest.LbEpInfo{
						// 3 local endpoints on network1
						{Address: "10.0.0.1", Weight: 6},
						{Address: "10.0.0.2", Weight: 6},
						{Address: "10.0.0.3", Weight: 6},
						// 1 endpoint on network4 with no gateway (i.e. directly accessible)
						{Address: "40.0.0.1", Weight: 6},
					},
					Weight: 24,
				},
			},
			wantWorkloadMetadata: []string{
				";ns;example;;cluster1a",
				";ns;example;;cluster1a",
				";ns;example;;cluster1b",
				";ns;example;;cluster4",
			},
		},
		{
			name:  "from_network1_cluster1b",
			proxy: makeProxy("network1", "cluster1b"),
			want: []xdstest.LocLbEpInfo{
				{
					LbEps: []xdstest.LbEpInfo{
						// 3 local endpoints on network1
						{Address: "10.0.0.1", Weight: 6},
						{Address: "10.0.0.2", Weight: 6},
						{Address: "10.0.0.3", Weight: 6},
						// 1 endpoint on network4 with no gateway (i.e. directly accessible)
						{Address: "40.0.0.1", Weight: 6},
					},
					Weight: 24,
				},
			},
			wantWorkloadMetadata: []string{
				";ns;example;;cluster1a",
				";ns;example;;cluster1a",
				";ns;example;;cluster1b",
				";ns;example;;cluster4",
			},
		},
		{
			name:  "from_network2_cluster2a",
			proxy: makeProxy("network2", "cluster2a"),
			want: []xdstest.LocLbEpInfo{
				{
					LbEps: []xdstest.LbEpInfo{
						// 1 local endpoint on network2
						{Address: "20.0.0.1", Weight: 6},
						{Address: "20.0.0.2", Weight: 6},
						{Address: "20.0.0.3", Weight: 6},
						// 1 endpoint on network4 with no gateway (i.e. directly accessible)
						{Address: "40.0.0.1", Weight: 6},
					},
					Weight: 24,
				},
			},
			wantWorkloadMetadata: []string{
				";ns;example;;cluster2a",
				";ns;example;;cluster2b",
				";ns;example;;cluster2b",
				";ns;example;;cluster4",
			},
		},
		{
			name:  "from_network2_cluster2b",
			proxy: makeProxy("network2", "cluster2b"),
			want: []xdstest.LocLbEpInfo{
				{
					LbEps: []xdstest.LbEpInfo{
						// 1 local endpoint on network2
						{Address: "20.0.0.1", Weight: 6},
						{Address: "20.0.0.2", Weight: 6},
						{Address: "20.0.0.3", Weight: 6},
						// 1 endpoint on network4 with no gateway (i.e. directly accessible)
						{Address: "40.0.0.1", Weight: 6},
					},
					Weight: 24,
				},
			},
			wantWorkloadMetadata: []string{
				";ns;example;;cluster2a",
				";ns;example;;cluster2b",
				";ns;example;;cluster2b",
				";ns;example;;cluster4",
			},
		},
		{
			name:  "from_network3_cluster3",
			proxy: makeProxy("network3", "cluster3"),
			want: []xdstest.LocLbEpInfo{
				{
					LbEps: []xdstest.LbEpInfo{
						// 1 endpoint on network4 with no gateway (i.e. directly accessible)
						{Address: "40.0.0.1", Weight: 6},
					},
					Weight: 6,
				},
			},
			wantWorkloadMetadata: []string{
				";ns;example;;cluster4",
			},
		},
		{
			name:  "from_network4_cluster4",
			proxy: makeProxy("network4", "cluster4"),
			want: []xdstest.LocLbEpInfo{
				{
					LbEps: []xdstest.LbEpInfo{
						// 1 local endpoint on network4
						{Address: "40.0.0.1", Weight: 6},
					},
					Weight: 6,
				},
			},
			wantWorkloadMetadata: []string{
				";ns;example;;cluster4",
			},
		},
	}

	for configType, cases := range mtlsCases {
		t.Run(configType, func(t *testing.T) {
			for name, pa := range cases {
				t.Run(name, func(t *testing.T) {
					cfgs := pa.Configs
					if pa.Config.Name != "" {
						cfgs = append(cfgs, pa.Config)
					}
					env := environment(t, cfgs...)
					var tests []networkFilterCase
					if pa.IsMtlsDisabled {
						tests = noCrossNetwork
					} else {
						tests = networkFiltered
					}
					runNetworkFilterTest(t, env, tests, pa.SubsetName)
				})
			}
		})
	}
}

func TestEndpointsByNetworkFilter_SkipLBWithHostname(t *testing.T) {
	test.SetForTest(t, &features.MultiNetworkGatewayAPI, true)
	test.SetForTest(t, &features.PreferHBONESend, true)
	//  - 1 IP gateway for network1
	//  - 1 DNS gateway for network2
	//  - 1 IP gateway for network3
	//  - 0 gateways for network4
	ds := environment(t)
	origServices := ds.Env().Services()
	origGateways := ds.Env().NetworkGateways()
	ds.MemRegistry.AddService(&model.Service{
		Hostname: "istio-ingressgateway.istio-system.svc.cluster.local",
		Attributes: model.ServiceAttributes{
			ClusterExternalAddresses: &model.AddressMap{
				Addresses: map[cluster.ID][]string{
					"cluster2a": {""},
					"cluster2b": {""},
				},
			},
		},
	})
	for _, svc := range origServices {
		ds.MemRegistry.AddService(svc)
	}
	ds.MemRegistry.AddGateways(origGateways...)
	// Also add a hostname-based Gateway, which will be rejected.
	ds.MemRegistry.AddGateways(model.NetworkGateway{
		Network: "network2",
		Addr:    "aeiou.scooby.do",
		Port:    80,
	})

	// Run the tests and ensure that the new gateway is never used.
	runNetworkFilterTest(t, ds, networkFiltered, "")
}

func TestEndpointsByNetworkFilter_AmbientMuiltiNetwork(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	test.SetForTest(t, &features.EnableAmbientWaypointMultiNetwork, true)
	env := environment(t)
	env.Env().InitNetworksManager(env.Discovery)
	var ambientNetworkFiltered = []networkFilterCase{
		{
			name:  "from_network1_cluster1a",
			proxy: makeWaypointProxy("network1", "cluster1a"),
			want: []xdstest.LocLbEpInfo{
				{
					LbEps: []xdstest.LbEpInfo{
						// 3 local endpoints on network1
						{Address: "10.0.0.1", Weight: 6},
						{Address: "10.0.0.2", Weight: 6},
						{Address: "10.0.0.3", Weight: 6},
						// 1 endpoint on network2, cluster2a
						{Address: "2.2.2.2", Weight: 6},
						// 2 endpoints on network2, cluster2b
						{Address: "2.2.2.20", Weight: 6},
						{Address: "2.2.2.21", Weight: 6},
						// 1 endpoint on network4 is not considered reachable in ambient mode without a gateway
					},
					Weight: 36,
				},
			},
			wantWorkloadMetadata: []string{
				";ns;example;;cluster1a",
				";ns;example;;cluster1a",
				";ns;example;;cluster1b",
				";;;;cluster2a",
				";;;;cluster2b",
				";;;;cluster2b",
			},
		},
		{
			name:  "from_network1_cluster1b",
			proxy: makeWaypointProxy("network1", "cluster1b"),
			want: []xdstest.LocLbEpInfo{
				{
					LbEps: []xdstest.LbEpInfo{
						// 3 local endpoints on network1
						{Address: "10.0.0.1", Weight: 6},
						{Address: "10.0.0.2", Weight: 6},
						{Address: "10.0.0.3", Weight: 6},
						// 1 endpoint on network2, cluster2a
						{Address: "2.2.2.2", Weight: 6},
						// 2 endpoints on network2, cluster2b
						{Address: "2.2.2.20", Weight: 6},
						{Address: "2.2.2.21", Weight: 6},
						// 1 endpoint on network4 is not considered reachable in ambient mode without a gateway
					},
					Weight: 36,
				},
			},
			wantWorkloadMetadata: []string{
				";ns;example;;cluster1a",
				";ns;example;;cluster1a",
				";ns;example;;cluster1b",
				";;;;cluster2a",
				";;;;cluster2b",
				";;;;cluster2b",
			},
		},
		{
			name:  "from_network2_cluster2a",
			proxy: makeWaypointProxy("network2", "cluster2a"),
			want: []xdstest.LocLbEpInfo{
				{
					LbEps: []xdstest.LbEpInfo{
						// 3 local endpoints in network2
						{Address: "20.0.0.1", Weight: 6},
						{Address: "20.0.0.2", Weight: 6},
						{Address: "20.0.0.3", Weight: 6},
						// Nothing on network1 since gateway there does not listen on HBONE port
						// 1 endpoint on network4 is not considered reachable in ambient mode without a gateway
					},
					Weight: 18,
				},
			},
			wantWorkloadMetadata: []string{
				";ns;example;;cluster2a",
				";ns;example;;cluster2b",
				";ns;example;;cluster2b",
			},
		},
	}
	// The tests below are calling the endpoints filter from each one of the
	// networks and examines the returned filtered endpoints
	runNetworkFilterTest(t, env, ambientNetworkFiltered, "")
}

type networkFilterCase struct {
	name                 string
	proxy                *model.Proxy
	want                 []xdstest.LocLbEpInfo
	wantWorkloadMetadata []string
}

// runNetworkFilterTest calls the endpoints filter from each one of the
// networks and examines the returned filtered endpoints
func runNetworkFilterTest(t *testing.T, ds *xds.FakeDiscoveryServer, tests []networkFilterCase, subset string) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cn := fmt.Sprintf("outbound|80|%s|example.ns.svc.cluster.local", subset)
			proxy := ds.SetupProxy(tt.proxy)
			b := endpoints.NewEndpointBuilder(cn, proxy, ds.PushContext())
			filtered := b.BuildClusterLoadAssignment(testShards()).Endpoints
			xdstest.CompareEndpointsOrFail(t, cn, filtered, tt.want)
			// check workload metadata
			var gotWorkloadMetadata []string
			for _, llbEndpoints := range filtered {
				for _, ep := range llbEndpoints.LbEndpoints {
					metadata := ep.Metadata.FilterMetadata[util.IstioMetadataKey].Fields["workload"].GetStringValue()
					gotWorkloadMetadata = append(gotWorkloadMetadata, metadata)
				}
			}
			if !slices.Equal(gotWorkloadMetadata, tt.wantWorkloadMetadata) {
				t.Errorf("incorrect workload metadata: got %v, want %v", gotWorkloadMetadata, tt.wantWorkloadMetadata)
			}

			b2 := endpoints.NewEndpointBuilder(cn, proxy, ds.PushContext())
			filtered2 := b2.BuildClusterLoadAssignment(testShards()).Endpoints
			if diff := cmp.Diff(filtered2, filtered, protocmp.Transform(), cmpopts.IgnoreUnexported(endpoints.LocalityEndpoints{})); diff != "" {
				t.Fatalf("output of EndpointsByNetworkFilter is non-deterministic: %v", diff)
			}
		})
	}
}

func TestEndpointsWithMTLSFilter(t *testing.T) {
	test.SetForTest(t, &features.PreferHBONESend, true)
	casesMtlsDisabled := []networkFilterCase{
		{
			name:  "from_network1_cluster1a",
			proxy: makeProxy("network1", "cluster1a"),
			want: []xdstest.LocLbEpInfo{
				{
					LbEps:  []xdstest.LbEpInfo{},
					Weight: 0,
				},
			},
		},
	}
	networkFilteredForSniDnatCluster := []networkFilterCase{}
	for _, testcase := range networkFiltered {
		testcase.want = slices.Clone(testcase.want)
		for i := range testcase.want {
			locLbEpInfo := &testcase.want[i]
			// HBONE endpoints are not included for SNI-DNAT clusters
			locLbEpInfo.LbEps = slices.Filter(locLbEpInfo.LbEps, func(e xdstest.LbEpInfo) bool {
				return e.Address != "10.0.0.3"
			})
			var weight uint32
			for _, e := range locLbEpInfo.LbEps {
				weight += e.Weight
			}
			locLbEpInfo.Weight = weight
		}
		networkFilteredForSniDnatCluster = append(networkFilteredForSniDnatCluster, testcase)
	}

	for configType, cases := range mtlsCases {
		t.Run(configType, func(t *testing.T) {
			for name, pa := range cases {
				t.Run(name, func(t *testing.T) {
					cfgs := pa.Configs
					if pa.Config.Name != "" {
						cfgs = append(cfgs, pa.Config)
					}
					env := environment(t, cfgs...)
					var tests []networkFilterCase
					if pa.IsMtlsDisabled {
						tests = casesMtlsDisabled
					} else {
						tests = networkFilteredForSniDnatCluster
					}
					runMTLSFilterTest(t, env, tests, pa.SubsetName)
				})
			}
		})
	}
}

func runMTLSFilterTest(t *testing.T, ds *xds.FakeDiscoveryServer, tests []networkFilterCase, subset string) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := ds.SetupProxy(tt.proxy)
			cn := fmt.Sprintf("outbound_.80_.%s_.example.ns.svc.cluster.local", subset)
			b := endpoints.NewEndpointBuilder(cn, proxy, ds.PushContext())
			filtered := b.BuildClusterLoadAssignment(testShards()).Endpoints
			xdstest.CompareEndpointsOrFail(t, cn, filtered, tt.want)

			b2 := endpoints.NewEndpointBuilder(cn, proxy, ds.PushContext())
			filtered2 := b2.BuildClusterLoadAssignment(testShards()).Endpoints
			if diff := cmp.Diff(filtered2, filtered, protocmp.Transform(), cmpopts.IgnoreUnexported(endpoints.LocalityEndpoints{})); diff != "" {
				t.Fatalf("output of EndpointsByNetworkFilter is non-deterministic: %v", diff)
			}
		})
	}
}

func makeProxy(nw network.ID, c cluster.ID) *model.Proxy {
	return &model.Proxy{
		Metadata: &model.NodeMetadata{
			Network:   nw,
			ClusterID: c,
		},
	}
}

func makeWaypointProxy(nw network.ID, c cluster.ID) *model.Proxy {
	proxy := makeProxy(nw, c)
	if proxy.Labels == nil {
		proxy.Labels = make(map[string]string)
	}
	proxy.Labels[label.GatewayManaged.Name] = constants.ManagedGatewayMeshControllerLabel
	proxy.Type = model.Waypoint
        return proxy
}

// environment defines the networks with:
//   - 1 gateway for network1
//   - 3 gateway for network2
//   - 1 gateway for network3
//   - 0 gateways for network4
func environment(t test.Failer, c ...config.Config) *xds.FakeDiscoveryServer {
	ds := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		Configs: c,
		Services: []*model.Service{{
			Hostname:   "example.ns.svc.cluster.local",
			Attributes: model.ServiceAttributes{Name: "example", Namespace: "ns"},
			Ports:      model.PortList{{Port: 80, Protocol: protocol.HTTP, Name: "http"}},
		}},
		Gateways: []model.NetworkGateway{
			// network1 has only 1 gateway in cluster1a and no HBONEPort, so this gateway
			// in sidecar mode will be used for endpoints in both cluster1a and cluster1b,
			// but ambient waypoint will not use it at all.
			{
				Network:   "network1",
				Cluster:   "cluster1a",
				Addr:      "1.1.1.1",
				Port:      80,
			},

			// network2 has one gateway in each cluster2a and cluster2b. When targeting a particular
			// endpoint, only the gateway for its cluster will be selected. Since the clusters do not
			// have the same number of endpoints, the weights for the gateways will be different.
			{
				Network: "network2",
				Cluster: "cluster2a",
				Addr:    "2.2.2.2",
				Port:    80,
				HBONEPort: 15008,
			},
			{
				Network:   "network2",
				Cluster:   "cluster2b",
				Addr:      "2.2.2.20",
				Port:      80,
				HBONEPort: 15008,
			},
			{
				Network:   "network2",
				Cluster:   "cluster2b",
				Addr:      "2.2.2.21",
				Port:      80,
				HBONEPort: 15008,
			},

			// network3 has a gateway in cluster3, but no endpoints.
			{
				Network:   "network3",
				Cluster:   "cluster3",
				Addr:      "3.3.3.3",
				Port:      443,
				HBONEPort: 15008,
			},
		},
	})
	return ds
}

// testShards creates endpoints to be handed to the filter:
//   - 2 endpoints in network1
//   - 1 endpoints in network2
//   - 0 endpoints in network3
//   - 1 endpoints in network4
//
// All endpoints are part of service example.ns.svc.cluster.local on port 80 (http).
func testShards() *model.EndpointIndex {
	shards := &model.EndpointShards{Shards: map[model.ShardKey][]*model.IstioEndpoint{
		// network1 has one endpoint in each cluster
		{Cluster: "cluster1a"}: {
			{Network: "network1", Addresses: []string{"10.0.0.1"}},
			{Network: "network1", Addresses: []string{"foo.bar"}}, // endpoint generated from ServiceEntry
			{
				Network: "network1", Addresses: []string{"10.0.0.3"}, // endpoint when using HBONE
				Labels: map[string]string{model.TunnelLabel: model.TunnelHTTP},
			},
		},
		{Cluster: "cluster1b"}: {
			{Network: "network1", Addresses: []string{"10.0.0.2", "2001:1::10.2"}},
		},

		// network2 has an imbalance of endpoints between its clusters
		{Cluster: "cluster2a"}: {
			{Network: "network2", Addresses: []string{"20.0.0.1"}},
		},
		{Cluster: "cluster2b"}: {
			{Network: "network2", Addresses: []string{"20.0.0.2"}},
			{Network: "network2", Addresses: []string{"20.0.0.3", "2001:1::20.3"}},
		},

		// network3 has no endpoints.

		// network4 has a single endpoint, but not gateway so it will be considered reachable
		// in sidecar mode, but not reachable in ambient mode, since in ambient mode we require
		// E/W gateway.
		{Cluster: "cluster4"}: {
			{Network: "network4", Addresses: []string{"40.0.0.1"}},
		},
	}}
	// apply common properties
	for sk, shard := range shards.Shards {
		for i, ep := range shard {
			ep.ServicePortName = "http"
			ep.Namespace = "ns"
			ep.HostName = "example.ns.svc.cluster.local"
			ep.EndpointPort = 8080
			ep.TLSMode = "istio"
			if ep.Labels == nil {
				ep.Labels = make(map[string]string)
			}
			ep.Labels["app"] = "example"
			ep.Locality.ClusterID = sk.Cluster
			shards.Shards[sk][i] = ep
		}
	}
	// convert to EndpointIndex
	index := model.NewEndpointIndex(model.NewXdsCache())
	for shardKey, testEps := range shards.Shards {
		svc, _ := index.GetOrCreateEndpointShard("example.ns.svc.cluster.local", "ns")
		svc.Lock()
		svc.Shards[shardKey] = testEps
		svc.Unlock()
	}
	return index
}
