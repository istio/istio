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
	"sort"
	"strconv"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/hashicorp/go-multierror"

	"istio.io/manager/model"
	"istio.io/manager/model/proxy/alphav1/config"
	"istio.io/manager/proxy"

	"io/ioutil"

	"github.com/hashicorp/errwrap"
)

type ingressWatcher struct {
	agent     proxy.Agent
	ctl       model.Controller
	discovery model.ServiceDiscovery
	registry  *model.IstioRegistry
	secrets   model.SecretRegistry
	secret    string
	namespace string
	mesh      *MeshConfig
}

// NewIngressWatcher creates a new ingress watcher instance with an agent
func NewIngressWatcher(discovery model.ServiceDiscovery, ctl model.Controller,
	registry *model.IstioRegistry, secrets model.SecretRegistry, mesh *MeshConfig,
	secret, namespace string,
) (Watcher, error) {

	out := &ingressWatcher{
		agent:     proxy.NewAgent(runEnvoy(mesh, "ingress"), cleanupEnvoy(mesh), 10, 100*time.Millisecond),
		ctl:       ctl,
		discovery: discovery,
		registry:  registry,
		secrets:   secrets,
		secret:    secret,
		namespace: namespace,
		mesh:      mesh,
	}

	if err := ctl.AppendConfigHandler(model.IngressRule, func(model.Key, proto.Message, model.Event) {
		out.reload()
	}); err != nil {
		return nil, err
	}

	// ingress rule listing depends on the service declaration being up to date
	if err := ctl.AppendServiceHandler(func(*model.Service, model.Event) {
		out.reload()
	}); err != nil {
		return nil, err
	}

	return out, nil
}

func (w *ingressWatcher) reload() {
	w.agent.ScheduleConfigUpdate(w.generateConfig())
}

func (w *ingressWatcher) Run(stop <-chan struct{}) {
	go w.agent.Run(stop)

	// Initialize envoy according to the current model state,
	// instead of waiting for the first event to arrive.
	// Note that this is currently done synchronously (blocking),
	// to avoid racing with controller events lurking around the corner.
	// This can be improved once we switch to a mechanism where reloads
	// are linearized (e.g., by a single goroutine reloader).
	w.reload()
	w.ctl.Run(stop)
}

func (w *ingressWatcher) generateConfig() *Config {
	rules := w.registry.IngressRules(w.namespace)

	// Phase 1: group rules by host
	rulesByHost := make(map[string][]*config.RouteRule, len(rules))
	for _, rule := range rules {
		host := "*"
		if rule.Match != nil {
			if authority, ok := rule.Match.HttpHeaders["authority"]; ok {
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
		routes := make([]*HTTPRoute, 0, len(hostRules))
		for _, rule := range hostRules {
			route, err := buildIngressRoute(rule)
			if err != nil {
				glog.Warningf("Error constructing Envoy route from ingress rule: %v", err)
				continue
			}
			routes = append(routes, route)
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

	rConfig := &HTTPRouteConfig{VirtualHosts: vhosts}

	listener := &Listener{
		Port:       80,
		BindToPort: true,
		Filters: []*NetworkFilter{
			{
				Type: "read",
				Name: HTTPConnectionManager,
				Config: HTTPFilterConfig{
					CodecType:   "auto",
					StatPrefix:  "http",
					AccessLog:   []AccessLog{{Path: DefaultAccessLog}},
					RouteConfig: rConfig,
					Filters: []HTTPFilter{
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

	// configure for HTTPS if provided with a secret name
	if w.secret != "" {
		sslContext := w.buildSSLContext()
		listener.Port = 443
		listener.SSLContext = sslContext
	}

	listeners := []*Listener{listener}
	clusters := Clusters(rConfig.filterClusters(func(cl *Cluster) bool { return true })).Normalize()

	return &Config{
		Listeners: listeners,
		Admin: Admin{
			AccessLogPath: DefaultAccessLog,
			Port:          w.mesh.AdminPort,
		},
		ClusterManager: ClusterManager{
			Clusters: clusters,
			SDS: &SDS{
				Cluster:        buildDiscoveryCluster(w.mesh.DiscoveryAddress, "sds"),
				RefreshDelayMs: 1000,
			},
		},
	}
}

// TODO: with multiple keys/certs, these will have to be dynamic.
const (
	certChainFile  = "/etc/envoy/tls.crt"
	privateKeyFile = "/etc/envoy/tls.key"
)

func (w *ingressWatcher) buildSSLContext() *SSLContext {
	var uri string
	if w.namespace == "" {
		uri = w.secret + ".default"
	} else {
		uri = fmt.Sprintf("%s.%s", w.secret, w.namespace)
	}

	err := w.writeTLS(uri)
	if err != nil {
		glog.Warning("Failed to get and save secrets. Envoy will crash and trigger a retry...")
	}

	return &SSLContext{
		CertChainFile:  certChainFile,
		PrivateKeyFile: privateKeyFile,
	}
}

func (w *ingressWatcher) writeTLS(uri string) error {
	s, err := w.secrets.GetSecret(uri)
	if err != nil {
		return errwrap.Wrap(fmt.Errorf("could not get secret %q", uri), err)
	}

	cert, exists := s["tls.crt"]
	if !exists {
		return fmt.Errorf("could not find tls.crt in secret %q", uri)
	}

	key, exists := s["tls.key"]
	if !exists {
		return fmt.Errorf("could not find tls.key in secret %q", uri)
	}

	// Write to files
	if err = ioutil.WriteFile(certChainFile, cert, 0755); err != nil {
		return err
	}
	if err = ioutil.WriteFile(privateKeyFile, key, 0755); err != nil {
		return err
	}

	return nil
}

// buildIngressRoute translates an ingress rule to an Envoy route
func buildIngressRoute(rule *config.RouteRule) (*HTTPRoute, error) {
	route := &HTTPRoute{
		Path:   "",
		Prefix: "/",
	}

	if rule.Match != nil && rule.Match.HttpHeaders != nil {
		if uri, ok := rule.Match.HttpHeaders[HeaderURI]; ok {
			switch m := uri.MatchType.(type) {
			case *config.StringMatch_Exact:
				route.Path = m.Exact
				route.Prefix = ""
			case *config.StringMatch_Prefix:
				route.Path = ""
				route.Prefix = m.Prefix
			case *config.StringMatch_Regex:
				return nil, fmt.Errorf("unsupported route match condition: regex")
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
			return nil, multierror.Append(fmt.Errorf("failed to extract routing rule destination port"), err)
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

	// Ensure all destination clusters have the same port number.
	//
	// This is currently required for doing host header rewrite (host:port),
	// which is scoped to the entire route.
	// This restriction can be relaxed by constructing multiple envoy.Route objects
	// per config.RouteRule, and doing weighted load balancing using Runtime.
	portSet := make(map[int]struct{}, 1)
	for _, cluster := range route.clusters {
		portSet[cluster.port.Port] = struct{}{}
	}
	if len(portSet) > 1 {
		return nil, fmt.Errorf("unsupported multiple destination ports per ingress route rule")
	}

	// Rewrite the host header so that inbound proxies can match incoming traffic
	route.HostRewrite = fmt.Sprintf("%s:%d", rule.Destination, route.clusters[0].port.Port)

	return route, nil
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
