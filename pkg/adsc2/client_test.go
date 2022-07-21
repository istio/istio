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

package adsc

import (
	"fmt"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/pkg/log"
)

var testCluster = &cluster.Cluster{
	Name:                 "test-eds",
	ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
	EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
		EdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_Ads{
				Ads: &core.AggregatedConfigSource{},
			},
		},
	},
	LbPolicy: cluster.Cluster_ROUND_ROBIN,
	TransportSocket: &core.TransportSocket{
		Name: wellknown.TransportSocketTLS,
		ConfigType: &core.TransportSocket_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&tls.UpstreamTlsContext{
				CommonTlsContext: &tls.CommonTlsContext{
					ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
						CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
							ValidationContextSdsSecretConfig: &tls.SdsSecretConfig{
								Name:      "kubernetes://test",
								SdsConfig: authn_model.SDSAdsConfig,
							},
						},
					},
				},
			}),
		},
	},
}

var testClusterNoSecret = &cluster.Cluster{
	Name:                 "test-eds",
	ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
	EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
		EdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_Ads{
				Ads: &core.AggregatedConfigSource{},
			},
		},
	},
	LbPolicy: cluster.Cluster_ROUND_ROBIN,
}

func TestClient(t *testing.T) {
	clusterHandler := Register(func(ctx HandlerContext, res *cluster.Cluster, event Event) {
		log.Infof("handle cluster %v: %v", event, res.Name)
		if event == EventDelete {
			return
		}
		ctx.RegisterDependency(v3.SecretType, xdstest.ExtractClusterSecretResources(t, res)...)
		ctx.RegisterDependency(v3.EndpointType, xdstest.ExtractEdsClusterNames([]*cluster.Cluster{res})...)
	})
	endpointsHandler := Register(func(ctx HandlerContext, res *endpoint.ClusterLoadAssignment, event Event) {
		log.Infof("handle endpoint %v: %v", event, res.ClusterName)
	})
	listenerHandler := Register(func(ctx HandlerContext, res *listener.Listener, event Event) {
		log.Infof("handle listener %v: %v", event, res.Name)
		if event == EventDelete {
			return
		}
		ctx.RegisterDependency(v3.SecretType, xdstest.ExtractListenerSecretResources(t, res)...)
		ctx.RegisterDependency(v3.RouteType, xdstest.ExtractRoutesFromListeners([]*listener.Listener{res})...)
		// TODO: ECDS
	})
	routesHandler := Register(func(ctx HandlerContext, res *route.RouteConfiguration, event Event) {
		log.Infof("handle route %v: %v", event, res.Name)
	})
	secretsHandler := Register(func(ctx HandlerContext, res *tls.Secret, event Event) {
		log.Infof("handle secret %v: %v", event, res.Name)
	})
	client := New(
		clusterHandler,
		Watch[*cluster.Cluster]("*"),
		listenerHandler,
		Watch[*listener.Listener]("*"),
		endpointsHandler,
		routesHandler,
		secretsHandler,
	)
	assert.NoError(t, client.Run(test.NewContext(t)))
	time.Sleep(time.Second * 3)
	fmt.Println(client.dumpTree())
}
