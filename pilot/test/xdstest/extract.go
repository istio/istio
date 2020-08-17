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

package xdstest

import (
	"reflect"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcpproxy "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"

	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/test"
)

func ExtractRoutesFromListeners(ll []*listener.Listener) []string {
	routes := []string{}
	for _, l := range ll {
		for _, fc := range l.FilterChains {
			for _, filter := range fc.Filters {
				if filter.Name == wellknown.HTTPConnectionManager {
					filter.GetTypedConfig()
					hcon := &hcm.HttpConnectionManager{}
					if err := ptypes.UnmarshalAny(filter.GetTypedConfig(), hcon); err != nil {
						panic(err)
					}
					switch r := hcon.GetRouteSpecifier().(type) {
					case *hcm.HttpConnectionManager_Rds:
						routes = append(routes, r.Rds.RouteConfigName)
					}
				}
			}
		}
	}
	return routes
}

func ExtractListenerNames(ll []*listener.Listener) []string {
	res := []string{}
	for _, l := range ll {
		res = append(res, l.Name)
	}
	return res
}

func ExtractListener(name string, ll []*listener.Listener) *listener.Listener {
	for _, l := range ll {
		if l.Name == name {
			return l
		}
	}
	return nil
}

func ExtractTCPProxy(t test.Failer, fcs *listener.FilterChain) *tcpproxy.TcpProxy {
	for _, fc := range fcs.Filters {
		if fc.Name == wellknown.TCPProxy {
			tcpProxy := &tcpproxy.TcpProxy{}
			if fc.GetTypedConfig() != nil {
				if err := ptypes.UnmarshalAny(fc.GetTypedConfig(), tcpProxy); err != nil {
					t.Fatalf("failed to unmarshal tcp proxy")
				}
			}
			return tcpProxy
		}
	}
	return nil
}

func ExtractEndpoints(endpoints []*endpoint.ClusterLoadAssignment) map[string][]string {
	got := map[string][]string{}
	for _, cla := range endpoints {
		if cla == nil {
			continue
		}
		for _, ep := range cla.Endpoints {
			for _, lb := range ep.LbEndpoints {
				if lb.GetEndpoint().Address.GetSocketAddress() != nil {
					got[cla.ClusterName] = append(got[cla.ClusterName], lb.GetEndpoint().Address.GetSocketAddress().Address)
				} else {
					got[cla.ClusterName] = append(got[cla.ClusterName], lb.GetEndpoint().Address.GetPipe().Path)
				}
			}
		}
	}
	return got
}

func ExtractCluster(name string, cc []*cluster.Cluster) *cluster.Cluster {
	for _, c := range cc {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func ExtractClusterEndpoints(clusters []*cluster.Cluster) map[string][]string {
	cla := []*endpoint.ClusterLoadAssignment{}
	for _, c := range clusters {
		cla = append(cla, c.LoadAssignment)
	}
	return ExtractEndpoints(cla)
}

func ExtractEdsClusterNames(cl []*cluster.Cluster) []string {
	res := []string{}
	for _, c := range cl {
		switch v := c.ClusterDiscoveryType.(type) {
		case *cluster.Cluster_Type:
			if v.Type != cluster.Cluster_EDS {
				continue
			}
		}
		res = append(res, c.Name)
	}
	return res
}

func ToDiscoveryResponse(p interface{}) *discovery.DiscoveryResponse {
	slice := InterfaceSlice(p)
	if len(slice) == 0 {
		return &discovery.DiscoveryResponse{}
	}
	resources := make([]*any.Any, 0, len(slice))
	for _, v := range slice {
		resources = append(resources, util.MessageToAny(v.(proto.Message)))
	}
	return &discovery.DiscoveryResponse{
		Resources: resources,
		TypeUrl:   resources[0].TypeUrl,
	}
}

func InterfaceSlice(slice interface{}) []interface{} {
	s := reflect.ValueOf(slice)
	if s.Kind() != reflect.Slice {
		panic("InterfaceSlice() given a non-slice type")
	}

	ret := make([]interface{}, s.Len())

	for i := 0; i < s.Len(); i++ {
		ret[i] = s.Index(i).Interface()
	}

	return ret
}
