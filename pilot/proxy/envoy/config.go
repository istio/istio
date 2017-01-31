// Copyright 2017 Google Inc.
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
	"strings"

	"istio.io/manager/model"

	multierror "github.com/hashicorp/go-multierror"

	"fmt"
)

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

const (
	// OutboundClusterPrefix is the prefix for service clusters external to the proxy instance
	OutboundClusterPrefix = "outbound:"

	// InboundClusterPrefix is the prefix for service clusters co-hosted on the proxy instance
	InboundClusterPrefix = "inbound:"
)

// TODO: these values used in the Envoy configuration will be configurable
const (
	Stdout           = "/dev/stdout"
	DefaultTimeoutMs = 1000
	DefaultLbType    = LbTypeRoundRobin
)

// Generate Envoy configuration for service instances co-located with Envoy and all services in the mesh
func Generate(instances []*model.ServiceInstance, services []*model.Service, mesh *MeshConfig) (*Config, error) {
	listeners, clusters := buildListeners(instances, services, mesh)
	// TODO: add catch-all filters to prevent Envoy from crashing
	listeners = append(listeners, Listener{
		Port:           mesh.ProxyPort,
		BindToPort:     true,
		UseOriginalDst: true,
		Filters:        make([]NetworkFilter, 0),
	})

	if len(mesh.MixerAddress) > 0 {
		clusters = append(clusters, Cluster{
			Name:             "mixer",
			Type:             "strict_dns",
			ConnectTimeoutMs: DefaultTimeoutMs,
			LbType:           DefaultLbType,
			Hosts: []Host{
				{
					URL: "tcp://" + mesh.MixerAddress,
				},
			},
		})
	}

	return &Config{
		Listeners: listeners,
		Admin: Admin{
			AccessLogPath: Stdout,
			Port:          mesh.AdminPort,
		},
		ClusterManager: ClusterManager{
			Clusters: clusters,
			SDS: SDS{
				Cluster: Cluster{
					Name:             "sds",
					Type:             "strict_dns",
					ConnectTimeoutMs: DefaultTimeoutMs,
					LbType:           DefaultLbType,
					Hosts: []Host{
						{
							URL: "tcp://" + mesh.DiscoveryAddress,
						},
					},
				},
				RefreshDelayMs: 1000,
			},
		},
	}, nil
}

// buildListeners uses iptables port redirect to route traffic either into the
// pod or outside the pod to service clusters based on the traffic metadata.
func buildListeners(instances []*model.ServiceInstance,
	services []*model.Service,
	mesh *MeshConfig) ([]Listener, []Cluster) {
	clusters := buildClusters(services)
	listeners := make([]Listener, 0)

	hostnames := make([][]string, 0)
	for _, instance := range instances {
		hostnames = append(hostnames, strings.Split(instance.Service.Hostname, "."))
	}
	suffix := sharedHost(hostnames...)

	// group by port values to service with the declared port
	type listener struct {
		instances map[model.Protocol][]*model.Service
		services  map[model.Protocol][]*model.Service
	}

	ports := make(map[int]*listener, 0)

	// helper function to work with multi-maps
	ensure := func(port int) {
		if _, ok := ports[port]; !ok {
			ports[port] = &listener{
				instances: make(map[model.Protocol][]*model.Service),
				services:  make(map[model.Protocol][]*model.Service),
			}
		}
	}

	// group all service instances by (target-)port values
	// (assumption: traffic gets redirected from service port to instance port)
	for _, instance := range instances {
		port := instance.Endpoint.Port
		ensure(port)
		ports[port].instances[instance.Endpoint.ServicePort.Protocol] = append(
			ports[port].instances[instance.Endpoint.ServicePort.Protocol], &model.Service{
				Hostname: instance.Service.Hostname,
				Address:  instance.Service.Address,
				Ports:    []*model.Port{instance.Endpoint.ServicePort},
			})
	}

	// group all services by (service-)port values for outgoing traffic
	for _, svc := range services {
		for _, port := range svc.Ports {
			ensure(port.Port)
			ports[port.Port].services[port.Protocol] = append(
				ports[port.Port].services[port.Protocol], &model.Service{
					Hostname: svc.Hostname,
					Address:  svc.Address,
					Ports:    []*model.Port{port},
				})
		}
	}

	// generate listener for each port
	for port, lst := range ports {
		listener := Listener{
			Port:       port,
			BindToPort: false,
		}

		// append localhost redirect cluster
		localhost := fmt.Sprintf("%s%d", InboundClusterPrefix, port)
		if len(lst.instances) > 0 {
			clusters = append(clusters, Cluster{
				Name:             localhost,
				Type:             "static",
				ConnectTimeoutMs: DefaultTimeoutMs,
				LbType:           DefaultLbType,
				Hosts:            []Host{{URL: fmt.Sprintf("tcp://%s:%d", "127.0.0.1", port)}},
			})
		}

		// Envoy uses L4 and L7 filters for TCP and HTTP traffic.
		// In practice, no port has two protocols used by both filters, but we
		// should be careful with not stepping on our feet.

		// The order of the filter insertion is important.
		if len(lst.instances[model.ProtocolTCP]) > 0 {
			listener.Filters = append(listener.Filters, NetworkFilter{
				Type: "read",
				Name: "tcp_proxy",
				Config: NetworkFilterConfig{
					Cluster:    localhost,
					StatPrefix: "inbound_tcp",
				},
			})
		}

		// TODO: TCP routing for outbound based on dst IP
		// TODO: HTTPS protocol for inbound and outbound configuration using TCP routing or SNI
		// TODO: if two service ports have same port or same target port values but
		// different names, we will get duplicate host routes.  Envoy prohibits
		// duplicate entries with identical domains.

		// For HTTP, the routing decision is based on the virtual host.
		hosts := make(map[string]VirtualHost, 0)
		for _, proto := range []model.Protocol{model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC} {
			for _, svc := range lst.services[proto] {
				host := buildHost(svc, OutboundClusterPrefix+svc.String(), suffix)
				hosts[svc.String()] = host
			}

			// If the traffic is sent to a service that has instances co-located with the proxy,
			// we choose the local service instance since we cannot distinguish between inbound and outbound packets.
			// Note that this may not be a problem if the service port and its endpoint port are distinct.
			for _, svc := range lst.instances[proto] {
				host := buildHost(svc, localhost, suffix)
				hosts[svc.String()] = host
			}
		}

		if len(hosts) > 0 {
			// sort hosts by key (should be non-overlapping domains)
			vhosts := make([]VirtualHost, 0)
			for _, host := range hosts {
				vhosts = append(vhosts, host)
			}
			sort.Sort(HostsByName(vhosts))

			listener.Filters = append(listener.Filters, NetworkFilter{
				Type: "read",
				Name: "http_connection_manager",
				Config: NetworkFilterConfig{
					CodecType:   "auto",
					StatPrefix:  "http",
					AccessLog:   []AccessLog{{Path: Stdout}},
					RouteConfig: RouteConfig{VirtualHosts: vhosts},
					Filters:     buildFilters(mesh),
				},
			})
		}

		if len(listener.Filters) > 0 {
			listeners = append(listeners, listener)
		}
	}

	sort.Sort(ListenersByPort(listeners))
	sort.Sort(ClustersByName(clusters))
	return listeners, clusters
}

// sharedHost computes the shared host name suffix for instances.
func sharedHost(parts ...[]string) []string {
	switch len(parts) {
	case 0:
		return nil
	case 1:
		return parts[0]
	default:
		// longest common suffix
		out := make([]string, 0)
		for i := 1; i <= len(parts[0]); i++ {
			part := ""
			all := true
			for j, host := range parts {
				hostpart := host[len(host)-i]
				if j == 0 {
					part = hostpart
				} else if part != hostpart {
					all = false
					break
				}
			}
			if all {
				out = append(out, part)
			} else {
				break
			}
		}

		// reverse
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		return out
	}
}

// buildHost constructs an entry for VirtualHost for a given service.
// Service contains name, namespace and a single port declaration.
func buildHost(svc *model.Service, cluster string, suffix []string) VirtualHost {
	hosts := make([]string, 0)
	domains := make([]string, 0)
	parts := strings.Split(svc.Hostname, ".")
	shared := sharedHost(suffix, parts)

	// if shared is "svc.cluster.local", then we can add "name.namespace", "name.namespace.svc", etc
	host := strings.Join(parts[0:len(parts)-len(shared)], ".")
	if len(host) > 0 {
		hosts = append(hosts, host)
	}
	for _, part := range shared {
		if len(host) > 0 {
			host = host + "."
		}
		host = host + part
		hosts = append(hosts, host)
	}

	// add cluster IP host name
	if len(svc.Address) > 0 {
		hosts = append(hosts, svc.Address)
	}

	// add ports
	if len(svc.Ports) > 0 {
		port := svc.Ports[0].Port
		for _, host := range hosts {
			domains = append(domains, fmt.Sprintf("%s:%d", host, port))

			// default port 80 does not need to be specified
			if port == 80 {
				domains = append(domains, host)
			}
		}
	}

	return VirtualHost{
		Name:    svc.String(),
		Domains: domains,
		Routes:  []Route{{Cluster: cluster}},
	}
}

// buildClusters creates a cluster for every (service, port)
func buildClusters(services []*model.Service) []Cluster {
	clusters := make([]Cluster, 0)
	for _, svc := range services {
		for _, port := range svc.Ports {
			clusterSvc := model.Service{
				Hostname: svc.Hostname,
				Ports:    []*model.Port{port},
			}
			cluster := Cluster{
				Name:             OutboundClusterPrefix + clusterSvc.String(),
				ServiceName:      clusterSvc.String(),
				Type:             "sds",
				LbType:           DefaultLbType,
				ConnectTimeoutMs: DefaultTimeoutMs,
			}
			if port.Protocol == model.ProtocolGRPC ||
				port.Protocol == model.ProtocolHTTP2 {
				cluster.Features = "http2"
			}
			clusters = append(clusters, cluster)
		}
	}
	sort.Sort(ClustersByName(clusters))
	return clusters
}

func buildFilters(mesh *MeshConfig) []Filter {
	filters := make([]Filter, 0)

	if len(mesh.MixerAddress) > 0 {
		filters = append(filters, Filter{
			Type: "both",
			Name: "esp",
			Config: FilterEndpointsConfig{
				ServiceConfig: "/etc/generic_service_config.json",
				ServerConfig:  "/etc/server_config.pb.txt",
			},
		})
	}

	filters = append(filters, Filter{
		Type:   "decoder",
		Name:   "router",
		Config: FilterRouterConfig{},
	})

	return filters
}
