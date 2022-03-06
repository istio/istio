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
	"fmt"
	"reflect"
	"sort"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcpproxy "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/proto"
	any "google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/sets"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/protomarshal"
)

func ExtractRoutesFromListeners(ll []*listener.Listener) []string {
	routes := []string{}
	for _, l := range ll {
		for _, fc := range l.FilterChains {
			for _, filter := range fc.Filters {
				if filter.Name == wellknown.HTTPConnectionManager {
					hcon := &hcm.HttpConnectionManager{}
					if err := filter.GetTypedConfig().UnmarshalTo(hcon); err != nil {
						continue
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

// ExtractSecretResources fetches all referenced SDS resource names from a list of clusters and listeners
func ExtractSecretResources(t test.Failer, rs []*any.Any) []string {
	resourceNames := sets.NewSet()
	for _, r := range rs {
		switch r.TypeUrl {
		case v3.ClusterType:
			c := &cluster.Cluster{}
			if err := r.UnmarshalTo(c); err != nil {
				t.Fatal(err)
			}
			sockets := []*core.TransportSocket{}
			if c.TransportSocket != nil {
				sockets = append(sockets, c.TransportSocket)
			}
			for _, ts := range c.TransportSocketMatches {
				sockets = append(sockets, ts.TransportSocket)
			}
			for _, s := range sockets {
				tl := &tls.UpstreamTlsContext{}
				if err := s.GetTypedConfig().UnmarshalTo(tl); err != nil {
					t.Fatal(err)
				}
				resourceNames.Insert(tl.GetCommonTlsContext().GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName())
				for _, s := range tl.GetCommonTlsContext().GetTlsCertificateSdsSecretConfigs() {
					resourceNames.Insert(s.GetName())
				}
			}
		case v3.ListenerType:
			l := &listener.Listener{}
			if err := r.UnmarshalTo(l); err != nil {
				t.Fatal(err)
			}
			sockets := []*core.TransportSocket{}
			for _, fc := range l.GetFilterChains() {
				if fc.GetTransportSocket() != nil {
					sockets = append(sockets, fc.GetTransportSocket())
				}
			}
			if ts := l.GetDefaultFilterChain().GetTransportSocket(); ts != nil {
				sockets = append(sockets, ts)
			}
			for _, s := range sockets {
				tl := &tls.DownstreamTlsContext{}
				if err := s.GetTypedConfig().UnmarshalTo(tl); err != nil {
					t.Fatal(err)
				}
				resourceNames.Insert(tl.GetCommonTlsContext().GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName())
				for _, s := range tl.GetCommonTlsContext().GetTlsCertificateSdsSecretConfigs() {
					resourceNames.Insert(s.GetName())
				}
			}
		}
	}
	resourceNames.Delete("")
	ls := resourceNames.UnsortedList()
	sort.Sort(sort.Reverse(sort.StringSlice(ls)))
	return ls
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

func ExtractVirtualHosts(rc *route.RouteConfiguration) map[string][]string {
	res := map[string][]string{}
	for _, vh := range rc.GetVirtualHosts() {
		var dests []string
		for _, r := range vh.Routes {
			if dc := r.GetRoute().GetCluster(); dc != "" {
				dests = append(dests, dc)
			}
		}
		sort.Strings(dests)
		for _, d := range vh.Domains {
			res[d] = dests
		}
	}
	return res
}

func ExtractRouteConfigurations(rc []*route.RouteConfiguration) map[string]*route.RouteConfiguration {
	res := map[string]*route.RouteConfiguration{}
	for _, l := range rc {
		res[l.Name] = l
	}
	return res
}

func ExtractListenerFilters(l *listener.Listener) map[string]*listener.ListenerFilter {
	res := map[string]*listener.ListenerFilter{}
	for _, lf := range l.ListenerFilters {
		res[lf.Name] = lf
	}
	return res
}

func ExtractFilterChain(name string, l *listener.Listener) *listener.FilterChain {
	for _, f := range l.GetFilterChains() {
		if f.GetName() == name {
			return f
		}
	}
	return nil
}

func ExtractTCPProxy(t test.Failer, fcs *listener.FilterChain) *tcpproxy.TcpProxy {
	for _, fc := range fcs.Filters {
		if fc.Name == wellknown.TCPProxy {
			tcpProxy := &tcpproxy.TcpProxy{}
			if fc.GetTypedConfig() != nil {
				if err := fc.GetTypedConfig().UnmarshalTo(tcpProxy); err != nil {
					t.Fatalf("failed to unmarshal tcp proxy: %v", err)
				}
			}
			return tcpProxy
		}
	}
	return nil
}

func ExtractHTTPConnectionManager(t test.Failer, fcs *listener.FilterChain) *hcm.HttpConnectionManager {
	for _, fc := range fcs.Filters {
		if fc.Name == wellknown.HTTPConnectionManager {
			h := &hcm.HttpConnectionManager{}
			if fc.GetTypedConfig() != nil {
				if err := fc.GetTypedConfig().UnmarshalTo(h); err != nil {
					t.Fatalf("failed to unmarshal hcm: %v", err)
				}
			}
			return h
		}
	}
	return nil
}

func ExtractLoadAssignments(cla []*endpoint.ClusterLoadAssignment) map[string][]string {
	got := map[string][]string{}
	for _, cla := range cla {
		if cla == nil {
			continue
		}
		got[cla.ClusterName] = append(got[cla.ClusterName], ExtractEndpoints(cla)...)
	}
	return got
}

func ExtractEndpoints(cla *endpoint.ClusterLoadAssignment) []string {
	if cla == nil {
		return nil
	}
	got := []string{}
	for _, ep := range cla.Endpoints {
		for _, lb := range ep.LbEndpoints {
			if lb.GetEndpoint().Address.GetSocketAddress() != nil {
				got = append(got, fmt.Sprintf("%s:%d", lb.GetEndpoint().Address.GetSocketAddress().Address, lb.GetEndpoint().Address.GetSocketAddress().GetPortValue()))
			} else {
				got = append(got, lb.GetEndpoint().Address.GetPipe().Path)
			}
		}
	}
	return got
}

func ExtractClusters(cc []*cluster.Cluster) map[string]*cluster.Cluster {
	res := map[string]*cluster.Cluster{}
	for _, c := range cc {
		res[c.Name] = c
	}
	return res
}

func ExtractCluster(name string, cc []*cluster.Cluster) *cluster.Cluster {
	return ExtractClusters(cc)[name]
}

func ExtractClusterEndpoints(clusters []*cluster.Cluster) map[string][]string {
	cla := []*endpoint.ClusterLoadAssignment{}
	for _, c := range clusters {
		cla = append(cla, c.LoadAssignment)
	}
	return ExtractLoadAssignments(cla)
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

func ExtractTLSSecrets(t test.Failer, secrets []*any.Any) map[string]*tls.Secret {
	res := map[string]*tls.Secret{}
	for _, a := range secrets {
		scrt := &tls.Secret{}
		if err := a.UnmarshalTo(scrt); err != nil {
			t.Fatal(err)
		}
		res[scrt.Name] = scrt
	}
	return res
}

func UnmarshalRouteConfiguration(t test.Failer, resp []*any.Any) []*route.RouteConfiguration {
	un := make([]*route.RouteConfiguration, 0, len(resp))
	for _, r := range resp {
		u := &route.RouteConfiguration{}
		if err := r.UnmarshalTo(u); err != nil {
			t.Fatal(err)
		}
		un = append(un, u)
	}
	return un
}

func UnmarshalClusterLoadAssignment(t test.Failer, resp []*any.Any) []*endpoint.ClusterLoadAssignment {
	un := make([]*endpoint.ClusterLoadAssignment, 0, len(resp))
	for _, r := range resp {
		u := &endpoint.ClusterLoadAssignment{}
		if err := r.UnmarshalTo(u); err != nil {
			t.Fatal(err)
		}
		un = append(un, u)
	}
	return un
}

func FilterClusters(cl []*cluster.Cluster, f func(c *cluster.Cluster) bool) []*cluster.Cluster {
	res := make([]*cluster.Cluster, 0, len(cl))
	for _, c := range cl {
		if f(c) {
			res = append(res, c)
		}
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

// DumpList will dump a list of protos. To workaround go type issues, call DumpList(t, InterfaceSlice([]proto.Message))
func DumpList(t test.Failer, protoList []interface{}) []string {
	res := []string{}
	for _, i := range protoList {
		p, ok := i.(proto.Message)
		if !ok {
			t.Fatalf("expected proto, got %T", i)
		}
		res = append(res, Dump(t, p))
	}
	return res
}

func Dump(t test.Failer, p proto.Message) string {
	v := reflect.ValueOf(p)
	if p == nil || (v.Kind() == reflect.Ptr && v.IsNil()) {
		return "nil"
	}
	s, err := protomarshal.ToJSONWithIndent(p, "  ")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func MapKeys(mp interface{}) []string {
	keys := reflect.ValueOf(mp).MapKeys()
	res := []string{}
	for _, k := range keys {
		res = append(res, k.String())
	}
	sort.Strings(res)
	return res
}
