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

package core

import (
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

const (
	headlessHost        = "kafka-brokers.kafka.svc.cluster.local"
	headlessTCPCluster  = "outbound|9091||" + headlessHost
	headlessHTTPCluster = "outbound|8080||" + headlessHost
)

type headlessPod struct {
	ip      string
	sidecar bool // has an Istio sidecar: tlsMode=istio and tunnel=http
	ambient bool // ambient-only: tunnel=http, no tlsMode
}

func headlessInstances(svc *model.Service, pods []headlessPod) []*model.ServiceInstance {
	var out []*model.ServiceInstance
	for _, p := range pods {
		for _, port := range svc.Ports {
			labels := map[string]string{}
			tlsMode := model.DisabledTLSModeLabel
			if p.sidecar {
				tlsMode = model.IstioMutualTLSModeLabel
			}
			if p.sidecar || p.ambient {
				labels[model.TunnelLabel] = model.TunnelHTTP
			}
			out = append(out, &model.ServiceInstance{
				Service:     svc,
				ServicePort: port,
				Endpoint: &model.IstioEndpoint{
					Addresses:       []string{p.ip},
					ServicePortName: port.Name,
					EndpointPort:    uint32(port.Port),
					Labels:          labels,
					TLSMode:         tlsMode,
				},
			})
		}
	}
	return out
}

func buildHeadlessHBONETest(t *testing.T, pods []headlessPod, dr *networking.DestinationRule) ([]*cluster.Cluster, []*listener.Listener) {
	svc := &model.Service{
		Hostname:       host.Name(headlessHost),
		DefaultAddress: constants.UnspecifiedIP,
		Ports: model.PortList{
			{Name: "tcp-replication", Port: 9091, Protocol: protocol.TCP},
			{Name: "http-admin", Port: 8080, Protocol: protocol.HTTP},
		},
		Resolution: model.Passthrough,
		Attributes: model.ServiceAttributes{
			Namespace:       "kafka",
			ServiceRegistry: provider.Kubernetes,
		},
	}
	var configs []config.Config
	if dr != nil {
		configs = append(configs, config.Config{
			Meta: config.Meta{GroupVersionKind: gvk.DestinationRule, Name: "dr", Namespace: "kafka"},
			Spec: dr,
		})
	}
	cg := NewConfigGenTest(t, TestOptions{
		Services:  []*model.Service{svc},
		Instances: headlessInstances(svc, pods),
		Configs:   configs,
	})
	proxy := cg.SetupProxy(&model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"10.0.0.99"},
		Metadata:    &model.NodeMetadata{Namespace: "kafka"},
	})
	return cg.Clusters(proxy), cg.Listeners(proxy)
}

func assertOriginalDst(t *testing.T, c *cluster.Cluster) {
	t.Helper()
	assert.Equal(t, c.GetType(), cluster.Cluster_ORIGINAL_DST)
	assert.Equal(t, isHeadlessHBONECluster(c), false)
}

func assertHeadlessHBONE(t *testing.T, c *cluster.Cluster) {
	t.Helper()
	assert.Equal(t, c.GetType(), cluster.Cluster_STATIC)
	assert.Equal(t, isHeadlessHBONECluster(c), true)
	assert.Equal(t, c.GetLbPolicy(), cluster.Cluster_ROUND_ROBIN)
	assert.Equal(t, c.GetLbConfig() == nil, true)
	assert.Equal(t, c.GetOutlierDetection() == nil, true)
	assert.Equal(t, len(c.GetTransportSocketMatches()), 0)
	assert.Equal(t, c.GetTransportSocket().GetName(), "envoy.transport_sockets.internal_upstream")
	assert.Equal(t, c.GetMetadata().GetFilterMetadata()[util.IstioMetadataKey].GetFields()["disable_mx"].GetBoolValue(), true)
}

func TestHeadlessHBONECluster(t *testing.T) {
	test.SetForTest(t, &features.EnableHBONESend, true)
	sidecars := []headlessPod{{ip: "10.0.0.10", sidecar: true}, {ip: "10.0.0.11", sidecar: true}}
	mixed := []headlessPod{{ip: "10.0.0.10", sidecar: true}, {ip: "10.0.0.11", ambient: true}}
	withPlain := []headlessPod{{ip: "10.0.0.10", sidecar: true}, {ip: "10.0.0.11", ambient: true}, {ip: "10.0.0.12"}}

	t.Run("flag off: mixed stays ORIGINAL_DST", func(t *testing.T) {
		test.SetForTest(t, &features.EnableHeadlessHBONESend, false)
		clusters, _ := buildHeadlessHBONETest(t, mixed, nil)
		assertOriginalDst(t, xdstest.ExtractCluster(headlessTCPCluster, clusters))
	})
	t.Run("all sidecars keep legacy mTLS", func(t *testing.T) {
		test.SetForTest(t, &features.EnableHeadlessHBONESend, true)
		clusters, _ := buildHeadlessHBONETest(t, sidecars, nil)
		assertOriginalDst(t, xdstest.ExtractCluster(headlessTCPCluster, clusters))
	})
	t.Run("all sidecars with PreferHBONESend use HBONE", func(t *testing.T) {
		test.SetForTest(t, &features.EnableHeadlessHBONESend, true)
		test.SetForTest(t, &features.PreferHBONESend, true)
		clusters, _ := buildHeadlessHBONETest(t, sidecars, nil)
		assertHeadlessHBONE(t, xdstest.ExtractCluster(headlessTCPCluster, clusters))
	})
	t.Run("sidecar plus ambient uses HBONE on TCP ports only", func(t *testing.T) {
		test.SetForTest(t, &features.EnableHeadlessHBONESend, true)
		clusters, _ := buildHeadlessHBONETest(t, mixed, nil)
		assertHeadlessHBONE(t, xdstest.ExtractCluster(headlessTCPCluster, clusters))
		assertOriginalDst(t, xdstest.ExtractCluster(headlessHTTPCluster, clusters))
	})
	t.Run("a pod outside the mesh keeps ORIGINAL_DST", func(t *testing.T) {
		test.SetForTest(t, &features.EnableHeadlessHBONESend, true)
		clusters, _ := buildHeadlessHBONETest(t, withPlain, nil)
		assertOriginalDst(t, xdstest.ExtractCluster(headlessTCPCluster, clusters))
	})
	t.Run("DestinationRule tls DISABLE is respected", func(t *testing.T) {
		test.SetForTest(t, &features.EnableHeadlessHBONESend, true)
		clusters, _ := buildHeadlessHBONETest(t, mixed, &networking.DestinationRule{
			Host: headlessHost,
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_DISABLE},
			},
		})
		assertOriginalDst(t, xdstest.ExtractCluster(headlessTCPCluster, clusters))
	})
	t.Run("DestinationRule outlier detection is cleared", func(t *testing.T) {
		test.SetForTest(t, &features.EnableHeadlessHBONESend, true)
		clusters, _ := buildHeadlessHBONETest(t, mixed, &networking.DestinationRule{
			Host: headlessHost,
			TrafficPolicy: &networking.TrafficPolicy{
				OutlierDetection: &networking.OutlierDetection{ConsecutiveGatewayErrors: &wrapperspb.UInt32Value{Value: 5}},
			},
		})
		assertHeadlessHBONE(t, xdstest.ExtractCluster(headlessTCPCluster, clusters))
	})
	t.Run("proxy with DisableHBONESend keeps ORIGINAL_DST", func(t *testing.T) {
		test.SetForTest(t, &features.EnableHeadlessHBONESend, true)
		svc := &model.Service{
			Hostname:   host.Name(headlessHost),
			Ports:      model.PortList{{Name: "tcp-replication", Port: 9091, Protocol: protocol.TCP}},
			Resolution: model.Passthrough,
			Attributes: model.ServiceAttributes{Namespace: "kafka", ServiceRegistry: provider.Kubernetes},
		}
		cg := NewConfigGenTest(t, TestOptions{Services: []*model.Service{svc}, Instances: headlessInstances(svc, mixed)})
		proxy := cg.SetupProxy(&model.Proxy{
			Type:        model.SidecarProxy,
			IPAddresses: []string{"10.0.0.99"},
			Metadata:    &model.NodeMetadata{Namespace: "kafka", DisableHBONESend: true},
		})
		assertOriginalDst(t, xdstest.ExtractCluster(headlessTCPCluster, cg.Clusters(proxy)))
	})
}

func TestHeadlessHBONEListenerFilterState(t *testing.T) {
	test.SetForTest(t, &features.EnableHBONESend, true)
	mixed := []headlessPod{{ip: "10.0.0.10", sidecar: true}, {ip: "10.0.0.11", ambient: true}}

	firstFilters := func(t *testing.T, listeners []*listener.Listener) []string {
		t.Helper()
		l := xdstest.ExtractListener("10.0.0.11_9091", listeners)
		if l == nil {
			t.Fatalf("per-pod listener 10.0.0.11_9091 not found in %v", xdstest.ExtractListenerNames(listeners))
		}
		var names []string
		for _, fc := range l.GetFilterChains() {
			names = append(names, fc.GetFilters()[0].GetName())
		}
		return names
	}

	t.Run("flag on: every chain records the dialed address first", func(t *testing.T) {
		test.SetForTest(t, &features.EnableHeadlessHBONESend, true)
		_, listeners := buildHeadlessHBONETest(t, mixed, nil)
		for _, name := range firstFilters(t, listeners) {
			assert.Equal(t, name, headlessHBONEOriginalDstFilter.GetName())
		}
	})
	t.Run("flag on, all sidecars: filter is still present", func(t *testing.T) {
		test.SetForTest(t, &features.EnableHeadlessHBONESend, true)
		_, listeners := buildHeadlessHBONETest(t, []headlessPod{{ip: "10.0.0.10", sidecar: true}, {ip: "10.0.0.11", sidecar: true}}, nil)
		for _, name := range firstFilters(t, listeners) {
			assert.Equal(t, name, headlessHBONEOriginalDstFilter.GetName())
		}
	})
	t.Run("flag off: no filter", func(t *testing.T) {
		test.SetForTest(t, &features.EnableHeadlessHBONESend, false)
		_, listeners := buildHeadlessHBONETest(t, mixed, nil)
		for _, name := range firstFilters(t, listeners) {
			if name == headlessHBONEOriginalDstFilter.GetName() {
				t.Fatalf("unexpected %s filter with the flag off", name)
			}
		}
	})
}
