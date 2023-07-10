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
	anypb "google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

func ExtractResource(res model.Resources) sets.String {
	s := sets.New[string]()
	for _, v := range res {
		s.Insert(v.Name)
	}
	return s
}

func ExtractRoutesFromListeners(ll []*listener.Listener) []string {
	routes := []string{}
	for _, l := range ll {
		for _, fc := range l.FilterChains {
			for _, filter := range fc.Filters {
				if filter.Name == wellknown.HTTPConnectionManager {
					h := SilentlyUnmarshalAny[hcm.HttpConnectionManager](filter.GetTypedConfig())
					switch r := h.GetRouteSpecifier().(type) {
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
func ExtractSecretResources(t test.Failer, rs []*anypb.Any) []string {
	resourceNames := sets.New[string]()
	for _, r := range rs {
		switch r.TypeUrl {
		case v3.ClusterType:
			c := UnmarshalAny[cluster.Cluster](t, r)
			sockets := []*core.TransportSocket{}
			if c.TransportSocket != nil {
				sockets = append(sockets, c.TransportSocket)
			}
			for _, ts := range c.TransportSocketMatches {
				sockets = append(sockets, ts.TransportSocket)
			}
			for _, s := range sockets {
				tl := UnmarshalAny[tls.UpstreamTlsContext](t, s.GetTypedConfig())
				resourceNames.Insert(tl.GetCommonTlsContext().GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName())
				for _, s := range tl.GetCommonTlsContext().GetTlsCertificateSdsSecretConfigs() {
					resourceNames.Insert(s.GetName())
				}
			}
		case v3.ListenerType:
			l := UnmarshalAny[listener.Listener](t, r)
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
				tl := UnmarshalAny[tls.DownstreamTlsContext](t, s.GetTypedConfig())
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

func SilentlyUnmarshalAny[T any](a *anypb.Any) *T {
	dst := any(new(T)).(proto.Message)
	if err := a.UnmarshalTo(dst); err != nil {
		var z *T
		return z
	}
	return any(dst).(*T)
}

func UnmarshalAny[T any](t test.Failer, a *anypb.Any) *T {
	dst := any(new(T)).(proto.Message)
	if err := a.UnmarshalTo(dst); err != nil {
		t.Fatalf("failed to unmarshal to %T: %v", dst, err)
	}
	return any(dst).(*T)
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

func ExtractFilterChainNames(l *listener.Listener) []string {
	res := []string{}
	for _, f := range l.GetFilterChains() {
		res = append(res, f.GetName())
	}
	return res
}

func ExtractFilterNames(t test.Failer, fcs *listener.FilterChain) ([]string, []string) {
	nwFilters := []string{}
	httpFilters := []string{}
	for _, fc := range fcs.Filters {
		if fc.Name == wellknown.HTTPConnectionManager {
			h := &hcm.HttpConnectionManager{}
			if fc.GetTypedConfig() != nil {
				if err := fc.GetTypedConfig().UnmarshalTo(h); err != nil {
					t.Fatalf("failed to unmarshal hcm: %v", err)
				}
			}
			for _, hf := range h.HttpFilters {
				httpFilters = append(httpFilters, hf.Name)
			}
		}
		nwFilters = append(nwFilters, fc.Name)
	}
	return nwFilters, httpFilters
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

func ExtractLocalityLbEndpoints(cla []*endpoint.ClusterLoadAssignment) map[string][]*endpoint.LocalityLbEndpoints {
	got := map[string][]*endpoint.LocalityLbEndpoints{}
	for _, cla := range cla {
		if cla == nil {
			continue
		}
		got[cla.ClusterName] = cla.Endpoints
	}
	return got
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

// ExtractHealthEndpoints returns all health and unhealth endpoints
func ExtractHealthEndpoints(cla *endpoint.ClusterLoadAssignment) ([]string, []string) {
	if cla == nil {
		return nil, nil
	}
	healthy := []string{}
	unhealthy := []string{}
	for _, ep := range cla.Endpoints {
		for _, lb := range ep.LbEndpoints {
			var addrString string
			switch lb.GetEndpoint().GetAddress().Address.(type) {
			case *core.Address_SocketAddress:
				addrString = fmt.Sprintf("%s:%d",
					lb.GetEndpoint().Address.GetSocketAddress().Address, lb.GetEndpoint().Address.GetSocketAddress().GetPortValue())
			case *core.Address_Pipe:
				addrString = lb.GetEndpoint().Address.GetPipe().Path
			case *core.Address_EnvoyInternalAddress:
				internalAddr := lb.GetEndpoint().Address.GetEnvoyInternalAddress().GetServerListenerName()
				destinationAddr := lb.GetMetadata().GetFilterMetadata()["tunnel"].GetFields()["destination"].GetStringValue()
				addrString = fmt.Sprintf("%s;%s", internalAddr, destinationAddr)
			}
			if lb.HealthStatus == core.HealthStatus_HEALTHY {
				healthy = append(healthy, addrString)
			} else {
				unhealthy = append(unhealthy, addrString)
			}
		}
	}
	return healthy, unhealthy
}

// ExtractEndpoints returns all endpoints in the load assignment (including unhealthy endpoints)
func ExtractEndpoints(cla *endpoint.ClusterLoadAssignment) []string {
	h, uh := ExtractHealthEndpoints(cla)
	h = append(h, uh...)
	return h
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

func ExtractTLSSecrets(t test.Failer, secrets []*anypb.Any) map[string]*tls.Secret {
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

func UnmarshalRouteConfiguration(t test.Failer, resp []*anypb.Any) []*route.RouteConfiguration {
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

func UnmarshalClusterLoadAssignment(t test.Failer, resp []*anypb.Any) []*endpoint.ClusterLoadAssignment {
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

func ToDiscoveryResponse[T proto.Message](p []T) *discovery.DiscoveryResponse {
	resources := make([]*anypb.Any, 0, len(p))
	for _, v := range p {
		resources = append(resources, protoconv.MessageToAny(v))
	}
	return &discovery.DiscoveryResponse{
		Resources: resources,
		TypeUrl:   resources[0].TypeUrl,
	}
}

// DumpList will dump a list of protos.
func DumpList[T any](t test.Failer, protoList []T) []string {
	res := []string{}
	for _, i := range protoList {
		p, ok := any(i).(proto.Message)
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

func MapKeys[M ~map[string]V, V any](mp M) []string {
	res := maps.Keys(mp)
	sort.Strings(res)
	return res
}
