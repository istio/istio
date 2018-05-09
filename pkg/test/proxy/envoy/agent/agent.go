//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package agent

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"regexp"

	envoy_api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoy_listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	envoy_route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	envoy_bootstrap "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v2"
	envoy_connection_manager "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	envoy_util "github.com/envoyproxy/go-control-plane/pkg/util"
	yaml2 "github.com/ghodss/yaml"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/jsonpb"
	"go.uber.org/multierr"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/server/echo"
	"istio.io/istio/pkg/test/proxy/envoy"
)

const (
	connectTimeout             = "0.25s"
	localAddress               = "127.0.0.1"
	httpStatPrefix             = "http"
	envoyHTTPConnectionManager = "envoy.http_connection_manager"
)

// PortConfig contains meta information about a port
type PortConfig struct {
	Name     string
	Protocol model.Protocol
}

// Config represents the configuration for an Agent
type Config struct {
	ServiceName string
	Ports       []PortConfig
	TLSCert     string
	TLSCKey     string
	Version     string
	TmpDir      string
}

// Port contains the runtime port mapping for a single configured port
type Port struct {
	Config      PortConfig
	EnvoyPort   int
	ServicePort int
}

// Agent bootstraps a local service/Envoy combination.
type Agent struct {
	Config         Config
	e              *envoy.Envoy
	app            *echo.Server
	ports          []Port
	envoyAdminPort int
}

// Start starts Envoy and the service.
func (a *Agent) Start() error {
	if err := a.startService(); err != nil {
		return err
	}
	return a.startEnvoy()
}

// Stop stops Envoy and the service.
func (a *Agent) Stop() error {
	var err error
	if a.e != nil {
		err = a.e.Stop()
	}
	if a.app != nil {
		err = multierr.Append(err, a.app.Stop())
	}
	return err
}

// GetPorts returns the list of runtime ports after the Agent has been started.
func (a *Agent) GetPorts() []Port {
	return a.ports
}

// GetEnvoyAdminPort returns the admin port for Envoy after the Agent has been started.
func (a *Agent) GetEnvoyAdminPort() int {
	return a.envoyAdminPort
}

func toEnvoyAddress(ip string, port uint32) *envoy_core.Address {
	return &envoy_core.Address{
		Address: &envoy_core.Address_SocketAddress{
			SocketAddress: &envoy_core.SocketAddress{
				Address: ip,
				PortSpecifier: &envoy_core.SocketAddress_PortValue{
					PortValue: uint32(port),
				},
			},
		},
	}
}

func (a *Agent) startService() error {
	httpPorts := make([]int, 0)
	grpcPorts := make([]int, 0)
	for _, port := range a.Config.Ports {
		switch port.Protocol {
		case model.ProtocolHTTP:
			httpPorts = append(httpPorts, 0)
			// TODO(nmittler): Add support for other protocols
		default:
			return fmt.Errorf("protocol %v not currently supported", port.Protocol)
		}
	}

	a.app = &echo.Server{
		HTTPPorts: httpPorts,
		GRPCPorts: grpcPorts,
		TLSCert:   a.Config.TLSCert,
		TLSCKey:   a.Config.TLSCKey,
		Version:   a.Config.Version,
	}
	return a.app.Start()
}

func (a *Agent) startEnvoy() error {
	envoyCfg, err := a.buildBootstrapConfig()
	if err != nil {
		return err
	}

	tmpDir := a.Config.TmpDir
	if tmpDir == "" {
		tmpDir, err = createTempDir()
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)
	}

	// Conver the protobuf to JSON.
	marshaller := jsonpb.Marshaler{}
	jsonStr, err := marshaller.MarshalToString(envoyCfg)
	if err != nil {
		return err
	}

	// Convert from JSON to YAML
	yamlBytes, err := yaml2.JSONToYAML([]byte(jsonStr))
	if err != nil {
		return err
	}

	// Hack to deal with the formatting expectation for durations in Envoy.
	re := regexp.MustCompile("connectTimeout: (.*)")
	match := re.FindSubmatch(yamlBytes)
	if len(match) > 0 {
		newValue := []byte(fmt.Sprintf("connectTimeout: %s", connectTimeout))
		yamlBytes = re.ReplaceAll(yamlBytes, newValue)
	}

	fmt.Println("NM: Envoy config:")
	fmt.Println(string(yamlBytes))

	// Write out the yaml file.
	outFile, err := createTempfile(tmpDir, "envoy_", ".yaml")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(outFile, yamlBytes, 0644)
	if err != nil {
		return err
	}

	a.e = &envoy.Envoy{
		ConfigFile: outFile,
	}

	return a.e.Start()
}

func (a *Agent) buildBootstrapConfig() (*envoy_bootstrap.Bootstrap, error) {
	adminPort, err := findFreePort()
	if err != nil {
		return nil, err
	}
	a.envoyAdminPort = adminPort

	envoyCfg := &envoy_bootstrap.Bootstrap{
		Admin: envoy_bootstrap.Admin{
			AccessLogPath: "/dev/null",
			Address:       *toEnvoyAddress(localAddress, uint32(adminPort)),
		},
		StaticResources: &envoy_bootstrap.Bootstrap_StaticResources{},
	}

	nextHTTPPort := 0
	for _, portCfg := range a.Config.Ports {
		if portCfg.Protocol == model.ProtocolHTTP {
			servicePort := a.app.HTTPPorts[nextHTTPPort]
			nextHTTPPort++

			// Create a cluster in Envoy to represent the service port.
			clusterName := fmt.Sprintf("%s_%d", a.Config.ServiceName, servicePort)
			cluster := envoy_api.Cluster{
				Name:           clusterName,
				ConnectTimeout: 1,
				Type:           envoy_api.Cluster_STATIC,
				LbPolicy:       envoy_api.Cluster_ROUND_ROBIN,
				Hosts: []*envoy_core.Address{
					toEnvoyAddress(localAddress, uint32(servicePort)),
				},
			}
			envoyCfg.StaticResources.Clusters = append(envoyCfg.StaticResources.Clusters, cluster)

			// Create a port for this listener.
			listenerPort, err := findFreePort()
			if err != nil {
				return nil, err
			}

			// Build a filter config that directs traffic to the service cluster.
			filterCfg, err := envoy_util.MessageToStruct(buildHTTPConnectionManager(clusterName))
			if err != nil {
				return nil, err
			}

			// Construct the listener config.
			listenerName := fmt.Sprintf("Inbound_%d_to_%s", listenerPort, clusterName)
			listener := envoy_api.Listener{
				// protocol is either TCP or HTTP
				Name:    listenerName,
				Address: *toEnvoyAddress(localAddress, uint32(listenerPort)),
				UseOriginalDst: &types.BoolValue{
					Value: true,
				},
				FilterChains: []envoy_listener.FilterChain{
					{
						FilterChainMatch: &envoy_listener.FilterChainMatch{},
						// TODO(nmittler): TlsContext: nil,
						Filters: []envoy_listener.Filter{
							{
								Name:   envoyHTTPConnectionManager,
								Config: filterCfg,
							},
						},
					},
				},
			}
			envoyCfg.StaticResources.Listeners = append(envoyCfg.StaticResources.Listeners, listener)

			// Add the port to the configuration.
			a.ports = append(a.ports, Port{
				Config:      portCfg,
				ServicePort: servicePort,
				EnvoyPort:   listenerPort,
			})
		}
	}
	return envoyCfg, nil
}

func buildHTTPConnectionManager(clusterName string) *envoy_connection_manager.HttpConnectionManager {
	connectionManager := &envoy_connection_manager.HttpConnectionManager{
		CodecType:  envoy_connection_manager.AUTO,
		StatPrefix: httpStatPrefix,
		HttpFilters: []*envoy_connection_manager.HttpFilter{
			{
				Name: envoy_util.CORS,
			},
			{
				Name: envoy_util.Router,
			},
		},
		RouteSpecifier: &envoy_connection_manager.HttpConnectionManager_RouteConfig{
			RouteConfig: &envoy_api.RouteConfiguration{
				Name: clusterName,
				VirtualHosts: []envoy_route.VirtualHost{
					{
						Name: clusterName,
						Domains: []string{
							"*",
						},
						Routes: []envoy_route.Route{
							{
								Match: envoy_route.RouteMatch{
									PathSpecifier: &envoy_route.RouteMatch_Prefix{
										Prefix: "/",
									},
								},
								Action: &envoy_route.Route_Route{
									Route: &envoy_route.RouteAction{
										ClusterSpecifier: &envoy_route.RouteAction_Cluster{
											Cluster: clusterName,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return connectionManager
}

func findFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func createTempDir() (string, error) {
	tmpDir, err := ioutil.TempDir(os.TempDir(), "agent_test")
	if err != nil {
		return "", err
	}
	return tmpDir, nil
}

func createTempfile(tmpDir, prefix, suffix string) (string, error) {
	f, err := ioutil.TempFile(tmpDir, prefix)
	if err != nil {
		return "", err
	}
	var tmpName string
	if tmpName, err = filepath.Abs(f.Name()); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Remove(tmpName); err != nil {
		return "", err
	}
	return tmpName + suffix, nil
}
