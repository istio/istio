// Copyright 2019 Istio Authors
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

package agent

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	netUrl "net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdsapiCore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	xdsapiListener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	envoyFilterHttp "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	envoyFilterTcp "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	envoyUtil "github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	googleProtobuf6 "github.com/gogo/protobuf/types"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/go-multierror"

	"google.golang.org/grpc"

	istio_networking_api "istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/envoy"
	"istio.io/istio/pkg/test/envoy/discovery"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/util/reserveport"
)

const (
	serviceNodeSeparator = "~"
	serviceCluster       = "local"
	proxyType            = "sidecar"
	maxStreams           = 100000
	listenerType         = "type.googleapis.com/envoy.api.v2.Listener"
	routeType            = "type.googleapis.com/envoy.api.v2.RouteConfiguration"
	clusterType          = "type.googleapis.com/envoy.api.v2.Cluster"
	localhost            = "127.0.0.1"
)

var (
	outboundHTTPListenerNamePattern = regexp.MustCompile("0.0.0.0_[0-9]+")

	serviceEntryCollection = metadata.IstioNetworkingV1alpha3Serviceentries.Collection.String()
)

// New creates new agent instance
func New(cfg Config) (*Instance, error) {
	var err error
	if cfg.PortManager == nil {
		cfg.PortManager, err = reserveport.NewPortManager()
		if err != nil {
			return nil, err
		}
	}

	// Create a temporary output directory if not provided.
	if cfg.TmpDir == "" {
		var err error
		cfg.TmpDir, err = createTempDir()
		if err != nil {
			return nil, err
		}
	}

	a := &Instance{
		cfg:          cfg,
		boundPortMap: make(map[uint32]int),
		envoy: &envoy.Envoy{
			LogLevel: cfg.EnvoyLogLevel,
		},
	}
	defer func() {
		if err != nil {
			_ = a.Close()
		}
	}()
	cfg.EchoServer.Config.Dialer = common.Dialer{
		GRPC:      a.dialGRPC,
		Websocket: a.dialWebsocket,
		HTTP:      a.doHTTP,
	}
	if err = cfg.EchoServer.Start(); err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}
	if err = a.start(); err != nil {
		return nil, err
	}
	return a, nil
}

// Instance manages the application and the sidecar proxy.
type Instance struct {
	cfg                       Config
	envoy                     *envoy.Envoy
	adminPort                 int
	ports                     []*MappedPort
	nodeID                    string
	yamlFile                  string
	ownedDir                  string
	discoveryFilterGrpcServer *grpc.Server
	discoveryFilter           *discovery.Filter
	discoveryFilterAddr       *net.TCPAddr
	boundPortMap              map[uint32]int
	serviceEntry              model.Config
}

// MappedPort provides a single port mapping between an Envoy proxy and its backend application.
type MappedPort struct {
	Name            string
	Protocol        model.Protocol
	ProxyPort       int
	ApplicationPort int
}

// GetConfig implements the agent.Instance interface.
func (a *Instance) GetConfig() model.Config {
	return a.serviceEntry
}

// GetAdminPort implements the agent.Instance interface.
func (a *Instance) GetAdminPort() int {
	return a.adminPort
}

// GetPorts implements the agent.Instance interface.
func (a *Instance) GetPorts() []*MappedPort {
	return a.ports
}

// FindFirstPortForProtocol is a utility method to simplify lookup of a port for a given protocol.
func (a *Instance) FindFirstPortForProtocol(protocol model.Protocol) (*MappedPort, error) {
	for _, port := range a.GetPorts() {
		if port.Protocol == protocol {
			return port, nil
		}
	}
	return nil, fmt.Errorf("no port found matching protocol %v", protocol)
}

// GetNodeID returns the envoy metadata ResourceID for pilot's service discovery.
func (a *Instance) GetNodeID() string {
	return a.nodeID
}

// CheckConfiguredForService implements the agent.Instance interface.
func (a *Instance) CheckConfiguredForService(target *Instance) error {
	cfg, err := envoy.GetConfigDump(a.GetAdminPort())
	if err != nil {
		return err
	}

	for _, port := range target.GetPorts() {
		clusterName := fmt.Sprintf("outbound|%d||%s.%s", port.ProxyPort, target.GetConfig().Name, a.fqd())
		if !envoy.IsClusterPresent(cfg, clusterName) {
			return fmt.Errorf("service %s missing config for cluster %s", a.GetConfig().Name, clusterName)
		}

		var listenerName string
		if port.Protocol.IsHTTP() {
			listenerName = fmt.Sprintf("0.0.0.0_%d", port.ProxyPort)
		} else {
			listenerName = fmt.Sprintf("127.0.0.1_%d", port.ProxyPort)
		}
		if !envoy.IsOutboundListenerPresent(cfg, listenerName) {
			return fmt.Errorf("service %s missing config for outbound listener %s", a.GetConfig().Name, listenerName)
		}

		if port.Protocol.IsHTTP() {
			if !envoy.IsOutboundRoutePresent(cfg, clusterName) {
				return fmt.Errorf("service %s missing route config to cluster %s", a.GetConfig().Name, clusterName)
			}
		}
	}

	return nil
}

// Close implements the agent.Instance interface.
func (a *Instance) Close() (err error) {
	if a.cfg.EchoServer != nil {
		err = a.cfg.EchoServer.Close()
	}
	if a.discoveryFilterGrpcServer != nil {
		a.discoveryFilterGrpcServer.Stop()
	}
	err = multierror.Append(err, a.envoy.Stop()).ErrorOrNil()
	if a.ownedDir != "" {
		_ = os.RemoveAll(a.ownedDir)
	} else if a.yamlFile != "" {
		_ = os.Remove(a.yamlFile)
	}
	// Free any reserved ports.
	if e := a.cfg.PortManager.Close(); e != nil {
		err = multierror.Append(err, e)
	}
	return
}

func (a *Instance) fqd() string {
	return fmt.Sprintf("%s.%s", a.cfg.Namespace.Name(), a.cfg.Domain)
}

// function for establishing GRPC connections from the application.
func (a *Instance) dialGRPC(ctx context.Context, address string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	// Modify the outbound URL being created by the application
	address = a.modifyClientURLString(address)
	return grpc.DialContext(ctx, address, opts...)
}

// function for establishing Websocket connections from the application.
func (a *Instance) dialWebsocket(dialer *websocket.Dialer, urlStr string, requestHeader http.Header) (*websocket.Conn, *http.Response, error) {
	// Modify the outbound URL being created by the application
	urlStr = a.modifyClientURLString(urlStr)
	return dialer.Dial(urlStr, requestHeader)
}

// function for making outbound HTTP requests from the application.
func (a *Instance) doHTTP(client *http.Client, req *http.Request) (*http.Response, error) {
	a.modifyClientURL(req.URL)
	return client.Do(req)
}

func (a *Instance) modifyClientURLString(url string) string {
	parsedURL, err := netUrl.Parse(url)
	if err != nil {
		// Failed to parse the URL, just use the original.
		return url
	}
	a.modifyClientURL(parsedURL)
	return parsedURL.String()
}

func (a *Instance) modifyClientURL(url *netUrl.URL) {
	port, err := strconv.Atoi(url.Port())
	if err != nil {
		// No port was specified. Nothing to do.
		return
	}

	boundPort, ok := a.boundPortMap[uint32(port)]
	if ok {
		url.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(boundPort))
	}
}

func (a *Instance) start() error {
	a.discoveryFilter = &discovery.Filter{
		DiscoveryAddr: a.cfg.DiscoveryAddress.String(),
		FilterFunc:    a.filterDiscoveryResponse,
	}

	// Start a GRPC server and register the handlers.
	a.discoveryFilterGrpcServer = grpc.NewServer(grpc.MaxConcurrentStreams(uint32(maxStreams)))
	// get the grpc server wired up
	grpc.EnableTracing = true
	a.discoveryFilter.Register(a.discoveryFilterGrpcServer)

	// Dynamically assign a port for the GRPC server.
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return err
	}

	// Start the gRPC server for the discovery filter.
	a.discoveryFilterAddr = listener.Addr().(*net.TCPAddr)
	go func() {
		if err = a.discoveryFilterGrpcServer.Serve(listener); err != nil {
			log.Warna(err)
		}
	}()

	// Generate the port mappings between Envoy and the backend service.
	a.adminPort, a.ports, err = a.createPorts(a.cfg.EchoServer.Config.Ports)
	if err != nil {
		return err
	}

	nodeID := generateServiceNode(a.cfg.ServiceName, a.cfg.Namespace.Name(), a.cfg.Domain)
	a.nodeID = nodeID

	// Create the YAML configuration file for Envoy.
	a.yamlFile, err = createEnvoyBootstrapFile(a.cfg.TmpDir, a.cfg.ServiceName, nodeID, a.adminPort, a.getDiscoveryPort(), a.ports)
	if err != nil {
		return err
	}

	// Start Envoy with the configuration
	logPrefix := fmt.Sprintf("[ENVOY-%s]", nodeID)
	a.envoy.YamlFile = a.yamlFile
	a.envoy.LogEntryPrefix = logPrefix
	if err = a.envoy.Start(); err != nil {
		return err
	}

	// Wait for Envoy to become healthy.
	if err = envoy.WaitForHealthCheckLive(a.adminPort); err != nil {
		return err
	}

	// Now add a service entry for this agent.
	serviceEntryYAML, err := createServiceEntryYAML(a)
	if err != nil {
		return err
	}

	// Apply the config to Galley.
	if err := a.cfg.Galley.ApplyConfig(a.cfg.Namespace, string(serviceEntryYAML)); err != nil {
		return err
	}

	// Wait for the ServiceEntry to be made available by Galley.
	mcpName := a.cfg.Namespace.Name() + "/" + a.cfg.ServiceName
	return a.cfg.Galley.WaitForSnapshot(serviceEntryCollection, func(actuals []*galley.SnapshotObject) error {
		for _, actual := range actuals {
			if actual.Metadata.Name == mcpName {
				se, ok := actual.Body.(*istio_networking_api.ServiceEntry)
				if !ok {
					return fmt.Errorf("received unexpected type %T for ServiceEntry %s", actual.Body, mcpName)
				}

				a.serviceEntry = model.Config{
					ConfigMeta: model.ConfigMeta{
						Type:      model.ServiceEntry.Type,
						Name:      a.cfg.ServiceName,
						Namespace: a.cfg.Namespace.Name(),
						Domain:    a.cfg.Domain,
						Labels:    actual.Metadata.Labels,
					},
					Spec: se,
				}
				return nil
			}
		}
		return fmt.Errorf("never received ServiceEntry %s from Galley", mcpName)
	})
}

func (a *Instance) getDiscoveryPort() int {
	return a.discoveryFilterAddr.Port
	// NOTE: uncomment this code to use use Pilot directly, without a filter.
	//addr, _ := net.ResolveTCPAddr("tcp", a.discoveryFilter.DiscoveryAddr)
	//return addr.Port
}

func generateServiceNode(serviceName, namespace, domain string) string {
	id := fmt.Sprintf("%s.%s", serviceName, randomBase64String(10))
	return strings.Join([]string{proxyType, localhost, id, namespace + "." + domain}, serviceNodeSeparator)
}

func isVirtualListener(l *xdsapi.Listener) bool {
	return l.Name == "virtual"
}

// nolint: staticcheck
func getTCPProxyClusterName(filter *xdsapiListener.Filter) (string, error) {
	// First, check if it's using the deprecated v1 format.
	config := filter.GetConfig()
	if config == nil {
		config, _ = envoyUtil.MessageToStruct(filter.GetTypedConfig())
	}
	fields := config.Fields
	deprecatedV1, ok := fields["deprecated_v1"]
	if ok && deprecatedV1.GetBoolValue() {
		v, ok := fields["value"]
		if !ok {
			return "", errors.New("value field missing")
		}
		v, ok = v.GetStructValue().Fields["route_config"]
		if !ok {
			return "", errors.New("route_config field missing")
		}
		v, ok = v.GetStructValue().Fields["routes"]
		if !ok {
			return "", errors.New("routes field missing")
		}
		vs := v.GetListValue().Values
		for _, v = range vs {
			v, ok = v.GetStructValue().Fields["cluster"]
			if ok {
				return v.GetStringValue(), nil
			}
		}
		return "", errors.New("cluster field missing")
	}

	cfg := &envoyFilterTcp.TcpProxy{}
	var err error
	if filter.GetConfig() != nil {
		err = envoyUtil.StructToMessage(filter.GetConfig(), cfg)
	} else {
		err = cfg.Unmarshal(filter.GetTypedConfig().GetValue())
	}
	if err != nil {
		return "", err
	}
	clusterSpec := cfg.ClusterSpecifier.(*envoyFilterTcp.TcpProxy_Cluster)
	if clusterSpec == nil {
		return "", fmt.Errorf("expected TCPProxy cluster")
	}
	return clusterSpec.Cluster, nil
}

// nolint: staticcheck
func isInboundListener(l *xdsapi.Listener) (bool, error) {
	for _, filter := range l.FilterChains[0].Filters {
		switch filter.Name {
		case envoyUtil.HTTPConnectionManager:
			cfg := &envoyFilterHttp.HttpConnectionManager{}
			var err error
			if filter.GetConfig() != nil {
				err = envoyUtil.StructToMessage(filter.GetConfig(), cfg)
			} else {
				err = cfg.Unmarshal(filter.GetTypedConfig().GetValue())
			}
			if err != nil {
				return false, err
			}
			rcfg := cfg.GetRouteConfig()
			if rcfg != nil {
				if strings.HasPrefix(rcfg.Name, "inbound") {
					return true, nil
				}
			}
			return false, nil
		case envoyUtil.TCPProxy:
			clusterName, err := getTCPProxyClusterName(&filter)
			if err != nil {
				return false, err
			}
			return strings.HasPrefix(clusterName, "inbound"), nil
		}
	}

	return false, fmt.Errorf("unable to determine whether the listener is inbound: %s", pb2Json(l))
}

func isOutboundListener(l *xdsapi.Listener) (bool, error) {
	for _, filter := range l.FilterChains[0].Filters {
		switch filter.Name {
		case envoyUtil.HTTPConnectionManager:
			return outboundHTTPListenerNamePattern.MatchString(l.Name), nil
		case envoyUtil.TCPProxy:
			clusterName, err := getTCPProxyClusterName(&filter)
			if err != nil {
				return false, err
			}
			return strings.HasPrefix(clusterName, "outbound"), nil
		}
	}

	return false, fmt.Errorf("unable to determine whether the listener is outbound: %s", pb2Json(l))
}

func pb2Json(pb proto.Message) string {
	m := jsonpb.Marshaler{
		Indent: "  ",
	}
	str, _ := m.MarshalToString(pb)
	return str
}

func (a *Instance) filterDiscoveryResponse(resp *xdsapi.DiscoveryResponse) (*xdsapi.DiscoveryResponse, error) {
	newResponse := xdsapi.DiscoveryResponse{
		TypeUrl:     resp.TypeUrl,
		Canary:      resp.Canary,
		VersionInfo: resp.VersionInfo,
		Nonce:       resp.Nonce,
	}

	for _, any := range resp.Resources {
		switch any.TypeUrl {
		case listenerType:
			l := &xdsapi.Listener{}
			if err := l.Unmarshal(any.Value); err != nil {
				return nil, err
			}

			if isVirtualListener(l) {
				// Exclude the iptables-mapped listener from the Envoy configuration. It's hard-coded to port 15001,
				// which will likely fail to be bound.
				continue
			}

			inbound, err := isInboundListener(l)
			if err != nil {
				return nil, err
			}
			if inbound {
				// This is a dynamic listener generated by Pilot for an inbound port. All inbound ports for the local
				// proxy are built into the static config, so we can safely ignore this listener.
				//
				// In addition, since we're using 127.0.0.1 as the IP address for all services/instances, the external
				// service registry's GetProxyServiceInstances() will mistakenly return instances for all services.
				// This is due to the fact that it uses IP address alone to map the instances. This results in Pilot
				// incorrectly generating inbound listeners for other services. These listeners shouldn't cause any
				// problems, but filtering them out here for correctness and clarity of the Envoy config.
				continue
			}

			outbound, err := isOutboundListener(l)
			if err != nil {
				return nil, err
			}
			if outbound {
				// Bind a real outbound port for this listener.
				if err := a.bindOutboundPort(&any, l); err != nil {
					return nil, err
				}
			}

		case clusterType:
			// Remove any management clusters.
			c := &xdsapi.Cluster{}
			if err := c.Unmarshal(any.Value); err != nil {
				return nil, err
			}
			switch {
			case strings.Contains(c.Name, "mgmtCluster"):
				continue
			}
		}
		newResponse.Resources = append(newResponse.Resources, any)
	}

	// Take a second pass to update routes to use updated listener ports.
	for i, any := range newResponse.Resources {
		switch any.TypeUrl {
		case routeType:
			r := &xdsapi.RouteConfiguration{}
			if err := r.Unmarshal(any.Value); err != nil {
				return nil, err
			}

			// Dynamic routes for outbound ports are named with their port.
			port, err := strconv.Atoi(r.Name)
			if err != nil {
				continue
			}

			// Look up the port to see if we have a custom bound port
			boundPort, ok := a.boundPortMap[uint32(port)]
			if !ok {
				continue
			}

			modified := false
			for i, vh := range r.VirtualHosts {
				for domainIndex, domain := range vh.Domains {
					parts := strings.Split(domain, ":")
					if len(parts) == 2 {
						modified = true
						r.VirtualHosts[i].Domains[domainIndex] = fmt.Sprintf("%s:%d", parts[0], boundPort)
					}
				}
			}

			if modified {
				// Update the resource.
				b, err := r.Marshal()
				if err != nil {
					return nil, err
				}
				newResponse.Resources[i].Value = b
			}
		}
	}

	return &newResponse, nil
}

func (a *Instance) bindOutboundPort(any *googleProtobuf6.Any, l *xdsapi.Listener) error {
	portFromPilot := l.Address.GetSocketAddress().GetPortValue()
	boundPort, ok := a.boundPortMap[portFromPilot]

	// Bind a real port for the outbound listener if we haven't already.
	if !ok {
		var err error
		boundPort, err = findFreePort(a.cfg.PortManager)
		if err != nil {
			return err
		}
		a.boundPortMap[portFromPilot] = boundPort
	}

	// Store the bound port in the listener.
	l.Address.GetSocketAddress().PortSpecifier.(*xdsapiCore.SocketAddress_PortValue).PortValue = uint32(boundPort)
	l.DeprecatedV1.BindToPort.Value = true

	// Output this content of the any.
	b, err := l.Marshal()
	if err != nil {
		return err
	}
	any.Value = b
	return nil
}

func (a *Instance) createPorts(servicePorts model.PortList) (adminPort int, mappedPorts []*MappedPort, err error) {
	if adminPort, err = findFreePort(a.cfg.PortManager); err != nil {
		return
	}

	mappedPorts = make([]*MappedPort, len(servicePorts))
	for i, servicePort := range servicePorts {
		var envoyPort int
		envoyPort, err = findFreePort(a.cfg.PortManager)
		if err != nil {
			return
		}

		mappedPorts[i] = &MappedPort{
			Name:            servicePort.Name,
			Protocol:        servicePort.Protocol,
			ApplicationPort: servicePort.Port,
			ProxyPort:       envoyPort,
		}
	}
	return
}

func findFreePort(portMgr reserveport.PortManager) (int, error) {
	reservedPort, err := portMgr.ReservePort()
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = reservedPort.Close()
	}()

	return int(reservedPort.GetPort()), nil
}

func randomBase64String(length int) string {
	buff := make([]byte, length)
	_, _ = rand.Read(buff)
	str := base64.URLEncoding.EncodeToString(buff)
	return str[:length]
}

func createTempDir() (string, error) {
	tmpDir, err := ioutil.TempDir(os.TempDir(), "istio_agent_test")
	if err != nil {
		return "", err
	}
	return tmpDir, nil
}
