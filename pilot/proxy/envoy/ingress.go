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
	agent   proxy.Agent
	ctl     model.Controller
	context *IngressConfig
}

// NewIngressWatcher creates a new ingress watcher instance with an agent
func NewIngressWatcher(ctl model.Controller, context *IngressConfig) (Watcher, error) {
	agent := proxy.NewAgent(runEnvoy(context.Mesh, "ingress"), cleanupEnvoy(context.Mesh), 10, 100*time.Millisecond)

	out := &ingressWatcher{
		agent:   agent,
		ctl:     ctl,
		context: context,
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
	w.agent.ScheduleConfigUpdate(generateIngress(w.context))
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

// IngressConfig defines information for ingress
type IngressConfig struct {
	// TODO: cert/key filenames will need to be dynamic for multiple key/cert pairs
	CertFile  string
	KeyFile   string
	Namespace string
	Secret    string
	Secrets   model.SecretRegistry
	Registry  *model.IstioRegistry
	Mesh      *MeshConfig
}

func generateIngress(conf *IngressConfig) *Config {
	rules := conf.Registry.IngressRules(conf.Namespace)

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
	sort.Slice(vhosts, func(i, j int) bool { return vhosts[i].Name < vhosts[j].Name })

	rConfig := &HTTPRouteConfig{VirtualHosts: vhosts}

	listener := &Listener{
		Address:    fmt.Sprintf("tcp://%s:80", WildcardAddress),
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
	if conf.Secret != "" {
		// configure Envoy
		listener.Address = fmt.Sprintf("tcp://%s:443", WildcardAddress)
		listener.SSLContext = &SSLContext{
			CertChainFile:  conf.CertFile,
			PrivateKeyFile: conf.KeyFile,
		}

		if err := writeTLS(conf.CertFile, conf.KeyFile, conf.Namespace,
			conf.Secret, conf.Secrets); err != nil {
			glog.Warning("Failed to get and save secrets. Envoy will crash and trigger a retry...")
		}
	}

	listeners := []*Listener{listener}
	clusters := rConfig.clusters().normalize()

	return &Config{
		Listeners: listeners,
		Admin: Admin{
			AccessLogPath: DefaultAccessLog,
			Address:       fmt.Sprintf("tcp://%s:%d", WildcardAddress, conf.Mesh.AdminPort),
		},
		ClusterManager: ClusterManager{
			Clusters: clusters,
			SDS: &SDS{
				Cluster:        buildDiscoveryCluster(conf.Mesh.DiscoveryAddress, "sds"),
				RefreshDelayMs: 1000,
			},
		},
	}
}

func writeTLS(certFilename, keyFilename, namespace, secret string, secrets model.SecretRegistry) error {
	var uri string
	if namespace == "" {
		uri = secret + ".default"
	} else {
		uri = fmt.Sprintf("%s.%s", secret, namespace)
	}

	s, err := secrets.GetSecret(uri)
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

	// write files
	if err = ioutil.WriteFile(certFilename, cert, 0755); err != nil {
		return err
	}
	if err = ioutil.WriteFile(keyFilename, key, 0755); err != nil {
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

		cluster := buildOutboundCluster(destination, port, nil, tags)
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
