// Copyright 2017 Istio Authors
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

package envoy

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	"istio.io/manager/model"
	"istio.io/manager/model/proxy/alphav1/config"
)

type ingressWatcher struct {
	agent     Agent
	discovery model.ServiceDiscovery
	registry  *model.IstioRegistry
	mesh      *MeshConfig
}

// NewIngressWatcher creates a new ingress watcher instance with an agent
func NewIngressWatcher(discovery model.ServiceDiscovery, ctl model.Controller,
	registry *model.IstioRegistry, mesh *MeshConfig, identity *ProxyNode) (Watcher, error) {

	out := &ingressWatcher{
		agent:     NewAgent(mesh.BinaryPath, mesh.ConfigPath, identity.Name),
		discovery: discovery,
		registry:  registry,
		mesh:      mesh,
	}

	err := ctl.AppendConfigHandler(model.IngressRule,
		func(model.Key, proto.Message, model.Event) { out.reload() })

	if err != nil {
		return nil, err
	}
	return out, nil
}

func (w *ingressWatcher) reload() {
	config, err := w.generateConfig()
	if err != nil {
		glog.Warningf("Failed to generate Envoy configuration: %v", err)
		return
	}

	current := w.agent.ActiveConfig()
	if reflect.DeepEqual(config, current) {
		glog.V(2).Info("Configuration is identical, skipping reload")
		return
	}

	// TODO: add retry logic
	if err := w.agent.Reload(config); err != nil {
		glog.Warningf("Envoy reload error: %v", err)
		return
	}

	// Add a short delay to de-risk a potential race condition in envoy hot reload code.
	// The condition occurs when the active Envoy instance terminates in the middle of
	// the Reload() function.
	time.Sleep(256 * time.Millisecond)
}

func (w *ingressWatcher) generateConfig() (*Config, error) {
	// TODO: Configurable namespace?
	rules := w.registry.IngressRules("")

	// Phase 1: group rules by host
	rulesByHost := make(map[string][]*config.RouteRule, len(rules))
	for _, rule := range rules {
		host := "*"
		if rule.Match != nil {
			if authority, ok := rule.Match.Http["authority"]; ok {
				switch match := authority.GetMatchType().(type) {
				case *config.StringMatch_Exact:
					host = match.Exact
				default:
					glog.Warningf("Unsupported match type for authority condition: %T", match)
				}
			}
		}

		rulesByHost[host] = append(rulesByHost[host], rule)
	}

	// Phase 2: create a VirtualHost for each host
	vhosts := make([]*VirtualHost, 0, len(rulesByHost))
	for host, hostRules := range rulesByHost {
		routes := make([]*Route, 0, len(hostRules))
		for _, rule := range hostRules {
			routes = append(routes, buildIngressRoute(rule))
		}
		sort.Sort(RoutesByPath(routes))
		vhost := &VirtualHost{
			Name:    host,
			Domains: []string{host},
			Routes:  routes,
		}
		vhosts = append(vhosts, vhost)
	}
	sort.Sort(HostsByName(vhosts))

	rConfig := &RouteConfig{VirtualHosts: vhosts}

	httpListener := &Listener{
		Port:       80,
		BindToPort: true,
		Filters: []*NetworkFilter{
			{
				Type: "read",
				Name: HTTPConnectionManager,
				Config: NetworkFilterConfig{
					CodecType:   "auto",
					StatPrefix:  "http",
					AccessLog:   []AccessLog{{Path: DefaultAccessLog}},
					RouteConfig: rConfig,
					Filters: []Filter{
						{
							Type:   "decoder",
							Name:   "router",
							Config: FilterRouterConfig{},
						},
					},
				},
			},
		},
	}

	// TODO: HTTPS listener
	listeners := []*Listener{httpListener}
	clusters := Clusters(rConfig.clusters()).Normalize()

	return &Config{
		Listeners: listeners,
		Admin: Admin{
			AccessLogPath: DefaultAccessLog,
			Port:          w.mesh.AdminPort,
		},
		ClusterManager: ClusterManager{
			Clusters: clusters,
			SDS: SDS{
				Cluster:        buildSDSCluster(w.mesh),
				RefreshDelayMs: 1000,
			},
		},
	}, nil

}

// buildIngressRoute translates an ingress rule to an Envoy route
func buildIngressRoute(rule *config.RouteRule) *Route {
	route := &Route{
		Path:   "",
		Prefix: "/",
	}

	if rule.Match != nil {
		if uri, ok := rule.Match.Http[HeaderURI]; ok {
			switch m := uri.MatchType.(type) {
			case *config.StringMatch_Exact:
				route.Path = m.Exact
				route.Prefix = ""
			case *config.StringMatch_Prefix:
				route.Path = ""
				route.Prefix = m.Prefix
			case *config.StringMatch_Regex:
				glog.Warningf("Unsupported route match condition: regex")
			}
		}
	}

	clusters := make([]*WeightedClusterEntry, 0)
	for _, dst := range rule.Route {
		// fetch route destination, or fallback to rule destination
		destination := dst.Destination
		if destination == "" {
			destination = rule.Destination
		}

		port, tags, err := extractPortAndTags(dst)
		if err != nil {
			glog.Warningf("Failed to extract routing rule destination port: %v", err)
			continue
		}

		cluster := buildOutboundCluster(destination, port, tags)
		clusters = append(clusters, &WeightedClusterEntry{
			Name:   cluster.Name,
			Weight: int(dst.Weight),
		})
		route.clusters = append(route.clusters, cluster)
	}
	route.WeightedClusters = &WeightedCluster{Clusters: clusters}

	// rewrite to a single cluster if it's one weighted cluster
	if len(rule.Route) == 1 {
		route.Cluster = route.WeightedClusters.Clusters[0].Name
		route.WeightedClusters = nil
	}

	return route
}

// extractPortAndTags extracts the destination service port from the given destination,
// as well as its tags (after clearing meta-tags describing the port).
// Note that this is a temporary measure to communicate the destination service's port
// to the proxy configuration generator. This can be improved by using
// a dedicated model object for IngressRule (instead of reusing RouteRule),
// which exposes the necessary target port field within the "Route" field.
func extractPortAndTags(dst *config.DestinationWeight) (*model.Port, model.Tags, error) {
	portNum, err := strconv.Atoi(dst.Tags["servicePort.port"])
	if err != nil {
		return nil, nil, err
	}
	portName, ok := dst.Tags["servicePort.name"]
	if !ok {
		return nil, nil, fmt.Errorf("no name specified for service port %d", portNum)
	}
	portProto, ok := dst.Tags["servicePort.protocol"]
	if !ok {
		return nil, nil, fmt.Errorf("no protocol specified for service port %d", portNum)
	}

	port := &model.Port{
		Port:     portNum,
		Name:     portName,
		Protocol: model.Protocol(portProto),
	}

	var tags model.Tags
	if len(dst.Tags) > 3 {
		tags = make(model.Tags, len(dst.Tags)-3)
		for k, v := range dst.Tags {
			tags[k] = v
		}
		delete(tags, "servicePort.port")
		delete(tags, "servicePort.name")
		delete(tags, "servicePort.protocol")
	}

	return port, tags, nil
}
