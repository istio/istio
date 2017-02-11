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
	"encoding/json"
	"io"
	"os"
	"sort"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/manager/model"
)

// Config generation main functions.
// The general flow of the generation process consists of the following steps:
// - routes are created for each destination, with referenced clusters stored as a special field
// - routes are grouped organized into listeners for inbound and outbound traffic
// - the outbound and inbound listeners are merged with preference given to the inbound traffic
// - clusters are aggregated and normalized.

// Requirements for the additions to the generation routines:
// - extra policies and filters should be added as additional passes over abstract config structures
// - lists in the config must be de-duplicated and ordered in a canonical way

// TODO: missing features in the config generation:
// - TCP routing
// - HTTPS protocol for inbound and outbound configuration using TCP routing or SNI
// - HTTP pod port collision creates duplicate virtual host entries
// - (bug) two service ports with the same target port create two virtual hosts with same domains
//   (not allowed by envoy). FIXME - need to detect and eliminate such ports in validation

// WriteFile saves config to a file
func (conf *Config) WriteFile(fname string) error {
	file, err := os.Create(fname)
	if err != nil {
		return err
	}

	if err := conf.Write(file); err != nil {
		err = multierror.Append(err, file.Close())
		return err
	}

	return file.Close()
}

func (conf *Config) Write(w io.Writer) error {
	out, err := json.MarshalIndent(&conf, "", "  ")
	if err != nil {
		return err
	}

	_, err = w.Write(out)
	return err
}

// Generate Envoy sidecar proxy configuration
func Generate(instances []*model.ServiceInstance, services []*model.Service,
	config *model.IstioRegistry, mesh *MeshConfig) *Config {
	listeners, clusters := build(instances, services, config, mesh)

	// inject mixer filter
	if mesh.MixerAddress != "" {
		insertMixerFilter(listeners, mesh.MixerAddress)
	}

	// TODO: re-implement
	_ = buildFaultFilters(nil, nil)

	// set bind to port values to values for port redirection
	for _, listener := range listeners {
		listener.BindToPort = false
	}

	// add an extra listener that binds to a port
	listeners = append(listeners, &Listener{
		Port:           mesh.ProxyPort,
		BindToPort:     true,
		UseOriginalDst: true,
		Filters:        make([]*NetworkFilter, 0),
	})

	// add SDS cluster
	return &Config{
		Listeners: listeners,
		Admin: Admin{
			AccessLogPath: DefaultAccessLog,
			Port:          mesh.AdminPort,
		},
		ClusterManager: ClusterManager{
			Clusters: clusters,
			SDS: SDS{
				Cluster:        buildSDSCluster(mesh),
				RefreshDelayMs: 1000,
			},
		},
	}
}

// build combines the outbound and inbound routes prioritizing the latter
func build(instances []*model.ServiceInstance, services []*model.Service,
	config *model.IstioRegistry, mesh *MeshConfig) ([]*Listener, Clusters) {
	outbound := buildOutboundFilters(instances, services, config, mesh)
	inbound := buildInboundFilters(instances)

	// merge the two sets of route configs
	configs := make(RouteConfigs)
	for port, config := range inbound {
		configs[port] = config
	}

	for port, outgoing := range outbound {
		if incoming, ok := configs[port]; ok {
			// If the traffic is sent to a service that has instances co-located with the proxy,
			// we choose the local service instance since we cannot distinguish between inbound and outbound packets.
			// Note that this may not be a problem if the service port and its endpoint port are distinct.
			configs[port] = incoming.merge(outgoing)
		} else {
			configs[port] = outgoing
		}
	}

	// canonicalize listeners and collect clusters
	clusters := make(Clusters, 0)
	listeners := make([]*Listener, 0)
	for port, config := range configs {
		sort.Sort(HostsByName(config.VirtualHosts))
		clusters = append(clusters, config.clusters()...)
		listener := &Listener{
			Port: port,
			Filters: []*NetworkFilter{{
				Type: "read",
				Name: HTTPConnectionManager,
				Config: NetworkFilterConfig{
					CodecType:  "auto",
					StatPrefix: "http",
					AccessLog: []AccessLog{{
						Path: DefaultAccessLog,
					}},
					RouteConfig: config,
					Filters: []Filter{{
						Type:   "decoder",
						Name:   "router",
						Config: FilterRouterConfig{},
					}},
				},
			}},
		}
		listeners = append(listeners, listener)
	}
	sort.Sort(ListenersByPort(listeners))
	return listeners, clusters.Normalize()
}

// buildOutboundFilters creates route configs indexed by ports for the traffic outbound
// from the proxy instance
func buildOutboundFilters(instances []*model.ServiceInstance, services []*model.Service,
	config *model.IstioRegistry, mesh *MeshConfig) RouteConfigs {
	// used for shortcut domain names for outbound hostnames
	suffix := sharedInstanceHost(instances)
	httpConfigs := make(RouteConfigs)

	// outbound connections/requests are redirected to service ports; we create a
	// map for each service port to define filters
	for _, service := range services {
		for _, port := range service.Ports {
			switch port.Protocol {
			case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
				routes := buildHTTPRoutes(service.Hostname, port, config)
				host := buildVirtualHost(service, port, suffix, routes)
				http := httpConfigs.EnsurePort(port.Port)
				http.VirtualHosts = append(http.VirtualHosts, host)
			default:
				glog.Warningf("Unsupported outbound protocol %v for port %d", port.Protocol, port)
			}
		}
	}

	return httpConfigs
}

// buildInboundFilters creates route configs indexed by ports for the traffic inbound
// to co-located service instances
func buildInboundFilters(instances []*model.ServiceInstance) RouteConfigs {
	// used for shortcut domain names for hostnames
	suffix := sharedInstanceHost(instances)
	httpConfigs := make(RouteConfigs)

	// inbound connections/requests are redirected to the endpoint port but appear to be sent
	// to the service port
	for _, instance := range instances {
		port := instance.Endpoint.ServicePort
		switch port.Protocol {
		case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
			cluster := buildInboundCluster(instance.Endpoint.Port, instance.Endpoint.ServicePort.Protocol)
			route := buildDefaultRoute(cluster)
			host := buildVirtualHost(instance.Service, port, suffix, []*Route{route})
			http := httpConfigs.EnsurePort(instance.Endpoint.Port)
			http.VirtualHosts = append(http.VirtualHosts, host)
		default:
			glog.Warningf("Unsupported inbound protocol %v for port %d", port.Protocol, port)
		}
	}

	return httpConfigs
}
