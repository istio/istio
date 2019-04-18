// Copyright 2018 Istio Authors
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

package adsc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"sync"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Config for the ADS connection.
type Config struct {
	// Namespace defaults to 'default'
	Namespace string

	// Workload defaults to 'test'
	Workload string

	// Meta includes additional metadata for the node
	Meta map[string]string

	// NodeType defaults to sidecar. "ingress" and "router" are also supported.
	NodeType string

	// IP is currently the primary key used to locate inbound configs. It is sent by client,
	// must match a known endpoint IP. Tests can use a ServiceEntry to register fake IPs.
	IP string
}

// ADSC implements a basic client for ADS, for use in stress tests and tools
// or libraries that need to connect to Istio pilot or other ADS servers.
type ADSC struct {
	// Stream is the GRPC connection stream, allowing direct GRPC send operations.
	// Set after Dial is called.
	stream ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient

	conn *grpc.ClientConn

	// NodeID is the node identity sent to Pilot.
	nodeID string

	done chan error

	certDir string
	url     string

	watchTime time.Time

	// InitialLoad tracks the time to receive the initial configuration.
	InitialLoad time.Duration

	// HTTPListeners contains received listeners with a http_connection_manager filter.
	HTTPListeners map[string]*xdsapi.Listener

	// TCPListeners contains all listeners of type TCP (not-HTTP)
	TCPListeners map[string]*xdsapi.Listener

	// All received clusters of type EDS, keyed by name
	EDSClusters map[string]*xdsapi.Cluster

	// All received clusters of no-EDS type, keyed by name
	Clusters map[string]*xdsapi.Cluster

	// All received routes, keyed by route name
	Routes map[string]*xdsapi.RouteConfiguration

	// All received endpoints, keyed by cluster name
	EDS map[string]*xdsapi.ClusterLoadAssignment

	// DumpCfg will print all received config
	DumpCfg bool

	// Metadata has the node metadata to send to pilot.
	// If nil, the defaults will be used.
	Metadata map[string]string

	// Updates includes the type of the last update received from the server.
	Updates     chan string
	VersionInfo map[string]string

	mutex sync.Mutex
}

const (
	typePrefix = "type.googleapis.com/envoy.api.v2."

	// Constants used for XDS

	// ClusterType is used for cluster discovery. Typically first request received
	clusterType = typePrefix + "Cluster"
	// EndpointType is used for EDS and ADS endpoint discovery. Typically second request.
	endpointType = typePrefix + "ClusterLoadAssignment"
	// ListenerType is sent after clusters and endpoints.
	listenerType = typePrefix + "Listener"
	// RouteType is sent after listeners.
	routeType = typePrefix + "RouteConfiguration"
)

var (
	// ErrTimeout is returned by Wait if no update is received in the given time.
	ErrTimeout = errors.New("timeout")
)

// Dial connects to a ADS server, with optional MTLS authentication if a cert dir is specified.
func Dial(url string, certDir string, opts *Config) (*ADSC, error) {
	adsc := &ADSC{
		done:        make(chan error),
		Updates:     make(chan string, 100),
		VersionInfo: map[string]string{},
		certDir:     certDir,
		url:         url,
	}
	if opts.Namespace == "" {
		opts.Namespace = "default"
	}
	if opts.NodeType == "" {
		opts.NodeType = "sidecar"
	}
	if opts.IP == "" {
		opts.IP = getPrivateIPIfAvailable().String()
	}
	if opts.Workload == "" {
		opts.Workload = "test-1"
	}
	adsc.Metadata = opts.Meta

	adsc.nodeID = fmt.Sprintf("%s~%s~%s.%s~%s.svc.cluster.local", opts.NodeType, opts.IP,
		opts.Workload, opts.Namespace, opts.Namespace)

	err := adsc.Run()
	return adsc, err
}

// Returns a private IP address, or unspecified IP (0.0.0.0) if no IP is available
func getPrivateIPIfAvailable() net.IP {
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		default:
			continue
		}
		if !ip.IsLoopback() {
			return ip
		}
	}
	return net.IPv4zero
}

func tlsConfig(certDir string) (*tls.Config, error) {
	clientCert, err := tls.LoadX509KeyPair(certDir+"/cert-chain.pem",
		certDir+"/key.pem")
	if err != nil {
		return nil, err
	}

	serverCABytes, err := ioutil.ReadFile(certDir + "/root-cert.pem")
	if err != nil {
		return nil, err
	}
	serverCAs := x509.NewCertPool()
	if ok := serverCAs.AppendCertsFromPEM(serverCABytes); !ok {
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      serverCAs,
		ServerName:   "istio-pilot.istio-system",
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			return nil
		},
	}, nil
}

// Close the stream.
func (a *ADSC) Close() {
	a.mutex.Lock()
	if a.stream != nil {
		_ = a.stream.CloseSend()
	}
	a.conn.Close()
	a.mutex.Unlock()
}

// Run will run the ADS client.
func (a *ADSC) Run() error {

	// TODO: pass version info, nonce properly
	var err error
	if len(a.certDir) > 0 {
		tlsCfg, err := tlsConfig(a.certDir)
		if err != nil {
			return err
		}
		creds := credentials.NewTLS(tlsCfg)

		opts := []grpc.DialOption{
			// Verify Pilot cert and service account
			grpc.WithTransportCredentials(creds),
		}
		a.conn, err = grpc.Dial(a.url, opts...)
		if err != nil {
			return err
		}
	} else {
		a.conn, err = grpc.Dial(a.url, grpc.WithInsecure())
		if err != nil {
			return err
		}
	}

	xds := ads.NewAggregatedDiscoveryServiceClient(a.conn)
	edsstr, err := xds.StreamAggregatedResources(context.Background())
	if err != nil {
		return err
	}
	a.stream = edsstr
	go a.handleRecv()
	return nil
}

func (a *ADSC) handleRecv() {
	for {
		msg, err := a.stream.Recv()
		if err != nil {
			log.Println("Connection closed ", err, a.nodeID)
			a.Close()
			a.WaitClear()
			a.Updates <- "close"
			return
		}

		listeners := []*xdsapi.Listener{}
		clusters := []*xdsapi.Cluster{}
		routes := []*xdsapi.RouteConfiguration{}
		eds := []*xdsapi.ClusterLoadAssignment{}
		for _, rsc := range msg.Resources { // Any
			a.VersionInfo[rsc.TypeUrl] = msg.VersionInfo
			valBytes := rsc.Value
			if rsc.TypeUrl == listenerType {
				ll := &xdsapi.Listener{}
				_ = proto.Unmarshal(valBytes, ll)
				listeners = append(listeners, ll)
			} else if rsc.TypeUrl == clusterType {
				ll := &xdsapi.Cluster{}
				_ = proto.Unmarshal(valBytes, ll)
				clusters = append(clusters, ll)
			} else if rsc.TypeUrl == endpointType {
				ll := &xdsapi.ClusterLoadAssignment{}
				_ = proto.Unmarshal(valBytes, ll)
				eds = append(eds, ll)
			} else if rsc.TypeUrl == routeType {
				ll := &xdsapi.RouteConfiguration{}
				_ = proto.Unmarshal(valBytes, ll)
				routes = append(routes, ll)
			}
		}

		// TODO: add hook to inject nacks
		a.mutex.Lock()
		a.ack(msg)
		a.mutex.Unlock()

		if len(listeners) > 0 {
			a.handleLDS(listeners)
		}
		if len(clusters) > 0 {
			a.handleCDS(clusters)
		}
		if len(eds) > 0 {
			a.handleEDS(eds)
		}
		if len(routes) > 0 {
			a.handleRDS(routes)
		}
	}

}

// nolint: staticcheck
func (a *ADSC) handleLDS(ll []*xdsapi.Listener) {
	lh := map[string]*xdsapi.Listener{}
	lt := map[string]*xdsapi.Listener{}

	routes := []string{}
	ldsSize := 0

	for _, l := range ll {
		ldsSize += l.Size()
		f0 := l.FilterChains[0].Filters[0]
		if f0.Name == "mixer" {
			f0 = l.FilterChains[0].Filters[1]
		}
		if f0.Name == "envoy.tcp_proxy" {
			lt[l.Name] = l
			config := f0.GetConfig()
			if config == nil {
				config, _ = xdsutil.MessageToStruct(f0.GetTypedConfig())
			}
			c := config.Fields["cluster"].GetStringValue()
			log.Printf("TCP: %s -> %s", l.Name, c)
		} else if f0.Name == "envoy.http_connection_manager" {
			lh[l.Name] = l

			// Getting from config is too painful..
			port := l.Address.GetSocketAddress().GetPortValue()
			if port == 15002 {
				routes = append(routes, "http_proxy")
			} else {
				routes = append(routes, fmt.Sprintf("%d", port))
			}
		} else if f0.Name == "envoy.mongo_proxy" {
			// ignore for now
		} else if f0.Name == "envoy.filters.network.mysql_proxy" {
			// ignore for now
		} else {
			tm := &jsonpb.Marshaler{Indent: "  "}
			log.Println(tm.MarshalToString(l))
		}
	}

	log.Println("LDS: http=", len(lh), "tcp=", len(lt), "size=", ldsSize)
	if a.DumpCfg {
		b, _ := json.MarshalIndent(ll, " ", " ")
		log.Println(string(b))
	}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if len(routes) > 0 {
		a.sendRsc(routeType, routes)
	}
	a.HTTPListeners = lh
	a.TCPListeners = lt

	select {
	case a.Updates <- "lds":
	default:
	}
}

// compact representations, for simplified debugging/testing

// TCPListener extracts the core elements from envoy Listener.
type TCPListener struct {
	// Address is the address, as expected by go Dial and Listen, including port
	Address string

	// LogFile is the access log address for the listener
	LogFile string

	// Target is the destination cluster.
	Target string
}

type Target struct {

	// Address is a go address, extracted from the mangled cluster name.
	Address string

	// Endpoints are the resolved endpoints from EDS or cluster static.
	Endpoints map[string]Endpoint
}

type Endpoint struct {
	// Weight extracted from EDS
	Weight int
}

// Save will save the json configs to files, using the base directory
func (a *ADSC) Save(base string) error {
	strResponse, err := json.MarshalIndent(a.TCPListeners, "  ", "  ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(base+"_lds_tcp.json", strResponse, 0644)
	if err != nil {
		return err
	}
	strResponse, err = json.MarshalIndent(a.HTTPListeners, "  ", "  ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(base+"_lds_http.json", strResponse, 0644)
	if err != nil {
		return err
	}
	strResponse, err = json.MarshalIndent(a.Routes, "  ", "  ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(base+"_rds.json", strResponse, 0644)
	if err != nil {
		return err
	}
	strResponse, err = json.MarshalIndent(a.EDSClusters, "  ", "  ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(base+"_ecds.json", strResponse, 0644)
	if err != nil {
		return err
	}
	strResponse, err = json.MarshalIndent(a.Clusters, "  ", "  ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(base+"_cds.json", strResponse, 0644)
	if err != nil {
		return err
	}
	strResponse, err = json.MarshalIndent(a.EDS, "  ", "  ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(base+"_eds.json", strResponse, 0644)
	if err != nil {
		return err
	}

	return err
}

func (a *ADSC) handleCDS(ll []*xdsapi.Cluster) {

	cn := []string{}
	cdsSize := 0
	edscds := map[string]*xdsapi.Cluster{}
	cds := map[string]*xdsapi.Cluster{}
	for _, c := range ll {
		cdsSize += c.Size()
		switch v := c.ClusterDiscoveryType.(type) {
		case *xdsapi.Cluster_Type:
			if v.Type != xdsapi.Cluster_EDS {
				cds[c.Name] = c
				continue
			}
		}
		cn = append(cn, c.Name)
		edscds[c.Name] = c
	}

	log.Println("CDS: ", len(cn), "size=", cdsSize)

	if len(cn) > 0 {
		a.sendRsc(endpointType, cn)
	}
	if a.DumpCfg {
		b, _ := json.MarshalIndent(ll, " ", " ")
		log.Println(string(b))
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.EDSClusters = edscds
	a.Clusters = cds

	select {
	case a.Updates <- "cds":
	default:
	}
}

func (a *ADSC) node() *core.Node {
	n := &core.Node{
		Id: a.nodeID,
	}
	if a.Metadata == nil {
		n.Metadata = &types.Struct{
			Fields: map[string]*types.Value{
				"ISTIO_PROXY_VERSION": {Kind: &types.Value_StringValue{StringValue: "1.0"}},
			}}
	} else {
		f := map[string]*types.Value{}

		for k, v := range a.Metadata {
			f[k] = &types.Value{Kind: &types.Value_StringValue{StringValue: v}}
		}
		n.Metadata = &types.Struct{
			Fields: f,
		}
	}
	return n
}

func (a *ADSC) Send(req *xdsapi.DiscoveryRequest) error {
	req.Node = a.node()
	req.ResponseNonce = time.Now().String()
	return a.stream.Send(req)
}

func (a *ADSC) handleEDS(eds []*xdsapi.ClusterLoadAssignment) {
	la := map[string]*xdsapi.ClusterLoadAssignment{}
	edsSize := 0
	ep := 0
	for _, cla := range eds {
		edsSize += cla.Size()
		la[cla.ClusterName] = cla
		ep += len(cla.Endpoints)
	}

	log.Println("EDS: ", len(eds), "size=", edsSize, "ep=", ep)
	if a.DumpCfg {
		b, _ := json.MarshalIndent(eds, " ", " ")
		log.Println(string(b))
	}
	if a.InitialLoad == 0 {
		// first load - Envoy loads listeners after endpoints
		_ = a.stream.Send(&xdsapi.DiscoveryRequest{
			ResponseNonce: time.Now().String(),
			Node:          a.node(),
			TypeUrl:       listenerType,
		})
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.EDS = la

	select {
	case a.Updates <- "eds":
	default:
	}
}

func (a *ADSC) handleRDS(configurations []*xdsapi.RouteConfiguration) {

	vh := 0
	rcount := 0
	size := 0

	rds := map[string]*xdsapi.RouteConfiguration{}

	for _, r := range configurations {
		for _, h := range r.VirtualHosts {
			vh++
			for _, rt := range h.Routes {
				rcount++
				// Example: match:<prefix:"/" > route:<cluster:"outbound|9154||load-se-154.local" ...
				log.Println(h.Name, rt.Match.PathSpecifier, rt.GetRoute().GetCluster())
			}
		}
		rds[r.Name] = r
		size += r.Size()
	}
	if a.InitialLoad == 0 {
		a.InitialLoad = time.Since(a.watchTime)
		log.Println("RDS: ", len(configurations), "size=", size, "vhosts=", vh, "routes=", rcount, " time=", a.InitialLoad)
	} else {
		log.Println("RDS: ", len(configurations), "size=", size, "vhosts=", vh, "routes=", rcount)
	}

	if a.DumpCfg {
		b, _ := json.MarshalIndent(configurations, " ", " ")
		log.Println(string(b))
	}

	a.mutex.Lock()
	a.Routes = rds
	a.mutex.Unlock()

	select {
	case a.Updates <- "rds":
	default:
	}

}

// WaitClear will clear the waiting events, so next call to Wait will get
// the next push type.
func (a *ADSC) WaitClear() {
	for {
		select {
		case <-a.Updates:
		default:
			return
		}
	}
}

// Wait for an update of the specified type. If type is empty, wait for next update.
func (a *ADSC) Wait(update string, to time.Duration) (string, error) {
	t := time.NewTimer(to)

	for {
		select {
		case t := <-a.Updates:
			if len(update) == 0 || update == t {
				return t, nil
			}
		case <-t.C:
			return "", ErrTimeout
		}
	}
}

// EndpointsJSON returns the endpoints, formatted as JSON, for debugging.
func (a *ADSC) EndpointsJSON() string {
	out, _ := json.MarshalIndent(a.EDS, " ", " ")
	return string(out)
}

// Watch will start watching resources, starting with LDS. Based on the LDS response
// it will start watching RDS and CDS.
func (a *ADSC) Watch() {
	a.watchTime = time.Now()
	_ = a.stream.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node:          a.node(),
		TypeUrl:       clusterType,
	})
}

func (a *ADSC) sendRsc(typeurl string, rsc []string) {
	_ = a.stream.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: "",
		Node:          a.node(),
		TypeUrl:       typeurl,
		ResourceNames: rsc,
	})
}

func (a *ADSC) ack(msg *xdsapi.DiscoveryResponse) {
	_ = a.stream.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: msg.Nonce,
		TypeUrl:       msg.TypeUrl,
		Node:          a.node(),
		VersionInfo:   msg.VersionInfo,
	})
}
