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

package ambient

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/util/assert"
)

func TestLocalK8sGatewayToNetworkGateways(t *testing.T) {
	ipType := gatewayv1.IPAddressType
	hostnameType := gatewayv1.HostnameAddressType
	passthroughMode := gatewayv1.TLSModePassthrough

	gateway := func(mutate func(gw *gatewayv1.Gateway)) *gatewayv1.Gateway {
		gw := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "eastwest",
				Namespace: "istio-system",
				Labels:    map[string]string{label.TopologyNetwork.Name: "nw2"},
			},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: constants.RemoteGatewayClassName,
				Listeners: []gatewayv1.Listener{{
					Name:     "hbone",
					Protocol: "HBONE",
					Port:     15008,
				}},
			},
			Status: gatewayv1.GatewayStatus{
				Addresses: []gatewayv1.GatewayStatusAddress{{Type: &ipType, Value: "1.2.3.4"}},
			},
		}
		if mutate != nil {
			mutate(gw)
		}
		return gw
	}

	base := model.NetworkGateway{
		Network:        "nw2",
		Cluster:        "cluster-1",
		Addr:           "1.2.3.4",
		HBONEPort:      15008,
		ServiceAccount: types.NamespacedName{Namespace: "istio-system", Name: "eastwest-istio-remote"},
	}
	source := types.NamespacedName{Namespace: "istio-system", Name: "eastwest"}

	cases := []struct {
		name   string
		gw     *gatewayv1.Gateway
		expect []NetworkGateway
	}{
		{
			name:   "hbone listener sets HBONEPort only",
			gw:     gateway(nil),
			expect: []NetworkGateway{{NetworkGateway: base, Source: source}},
		},
		{
			name: "hostname address",
			gw: gateway(func(gw *gatewayv1.Gateway) {
				gw.Status.Addresses = []gatewayv1.GatewayStatusAddress{{Type: &hostnameType, Value: "ew.example.com"}}
			}),
			expect: []NetworkGateway{{
				NetworkGateway: func() model.NetworkGateway {
					g := base
					g.Addr = "ew.example.com"
					return g
				}(),
				Source: source,
			}},
		},
		{
			name: "multiple hbone listeners collapse to the first",
			gw: gateway(func(gw *gatewayv1.Gateway) {
				gw.Spec.Listeners = append(gw.Spec.Listeners, gatewayv1.Listener{
					Name:     "hbone-alt",
					Protocol: "HBONE",
					Port:     15009,
				})
			}),
			expect: []NetworkGateway{{NetworkGateway: base, Source: source}},
		},
		{
			name: "auto-passthrough listener is not a local gateway",
			gw: gateway(func(gw *gatewayv1.Gateway) {
				gw.Spec.Listeners = []gatewayv1.Listener{{
					Name: "mtls",
					Port: 15443,
					TLS: &gatewayv1.ListenerTLSConfig{
						Mode: &passthroughMode,
						Options: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
							constants.ListenerModeOption: constants.ListenerModeAutoPassthrough,
						},
					},
				}}
			}),
			expect: []NetworkGateway{},
		},
		{
			name: "no network label",
			gw: gateway(func(gw *gatewayv1.Gateway) {
				gw.Labels = nil
			}),
			expect: nil,
		},
		{
			name: "wrong gateway class",
			gw: gateway(func(gw *gatewayv1.Gateway) {
				gw.Spec.GatewayClassName = constants.EastWestGatewayClassName
			}),
			expect: nil,
		},
		{
			name: "address with no type",
			gw: gateway(func(gw *gatewayv1.Gateway) {
				gw.Status.Addresses = []gatewayv1.GatewayStatusAddress{{Value: "1.2.3.4"}}
			}),
			expect: []NetworkGateway{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, localK8sGatewayToNetworkGateways(cluster.ID("cluster-1"), tc.gw), tc.expect)
		})
	}

	t.Run("remote cluster uses the remote conversion", func(t *testing.T) {
		assert.Equal(t, k8sGatewayToNetworkGateways(cluster.ID("cluster-2"), gateway(nil), cluster.ID("cluster-1")), nil)
	})
}
