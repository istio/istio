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
	"time"

	"github.com/golang/glog"

	"istio.io/api/proxy/v1/config"
	"istio.io/manager/model"
	"istio.io/manager/proxy"
)

type egressWatcher struct {
	agent   proxy.Agent
	ctl     model.Controller
	context *EgressConfig
}

// NewEgressWatcher creates a new egress watcher instance with an agent
func NewEgressWatcher(ctl model.Controller, context *EgressConfig) (Watcher, error) {
	agent := proxy.NewAgent(runEnvoy(context.Mesh, "egress"), cleanupEnvoy(context.Mesh), 10, 100*time.Millisecond)

	out := &egressWatcher{
		agent:   agent,
		ctl:     ctl,
		context: context,
	}

	// egress depends on the external services declaration being up to date
	if err := ctl.AppendServiceHandler(func(*model.Service, model.Event) {
		out.reload()
	}); err != nil {
		return nil, err
	}

	return out, nil
}

func (w *egressWatcher) reload() {
	w.agent.ScheduleConfigUpdate(generateEgress(w.context))
}

func (w *egressWatcher) Run(stop <-chan struct{}) {
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

// EgressConfig defines information for engress
type EgressConfig struct {
	Namespace string
	Services  model.ServiceDiscovery
	Mesh      *config.ProxyMeshConfig
	Port      int
}

func generateEgress(conf *EgressConfig) *Config {
	// Create a VirtualHost for each external service
	vhosts := make([]*VirtualHost, 0)
	services := conf.Services.Services()
	for _, service := range services {
		if service.ExternalName != "" {
			if host := buildEgressHTTPRoute(service); host != nil {
				vhosts = append(vhosts, host)
			}
		}
	}

	sort.Slice(vhosts, func(i, j int) bool { return vhosts[i].Name < vhosts[j].Name })

	rConfig := &HTTPRouteConfig{VirtualHosts: vhosts}

	// External services are expected to have only one external port or they will get conflated
	listener := &Listener{
		Address:    fmt.Sprintf("tcp://%s:%d", WildcardAddress, conf.Port),
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

	listeners := []*Listener{listener}
	clusters := rConfig.clusters().normalize()

	clusters.setTimeout(conf.Mesh.ConnectTimeout)

	return &Config{
		Listeners: listeners,
		Admin: Admin{
			AccessLogPath: DefaultAccessLog,
			Address:       fmt.Sprintf("tcp://%s:%d", WildcardAddress, conf.Mesh.ProxyAdminPort),
		},
		ClusterManager: ClusterManager{
			Clusters: clusters,
		},
	}
}

// buildEgressRoute translates an egress rule to an Envoy route
func buildEgressHTTPRoute(svc *model.Service) *VirtualHost {
	var host *VirtualHost

	for _, servicePort := range svc.Ports {
		protocol := servicePort.Protocol
		switch protocol {
		case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
			route := &HTTPRoute{
				Prefix:          "/",
				Cluster:         buildEgressClusterName(svc.ExternalName, servicePort.Port),
				AutoHostRewrite: true,
			}
			cluster := buildOutboundCluster(svc.Hostname, servicePort, nil)

			// configure cluster for strict_dns
			cluster.Name = buildEgressClusterName(svc.ExternalName, servicePort.Port)
			cluster.ServiceName = ""
			cluster.Type = ClusterTypeStrictDNS
			cluster.Hosts = []Host{
				{
					URL: fmt.Sprintf("tcp://%s:%d", svc.ExternalName, servicePort.Port),
				},
			}

			route.clusters = append(route.clusters, cluster)

			host = &VirtualHost{
				Name:    cluster.Name,
				Domains: []string{svc.Hostname},
				Routes:  []*HTTPRoute{route},
			}

		default:
			glog.Warningf("Unsupported outbound protocol %v for port %#v", protocol, servicePort)
		}
	}

	return host
}

func buildEgressClusterName(address string, port int) string {
	return fmt.Sprintf("%v|%d", address, port)
}
