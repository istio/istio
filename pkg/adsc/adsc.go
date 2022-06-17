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

package adsc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	any "google.golang.org/protobuf/types/known/anypb"
	pstruct "google.golang.org/protobuf/types/known/structpb"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/api/mesh/v1alpha1"
	mem "istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/pkg/log"
)

const (
	defaultClientMaxReceiveMessageSize = math.MaxInt32
	defaultInitialConnWindowSize       = 1024 * 1024 // default gRPC InitialWindowSize
	defaultInitialWindowSize           = 1024 * 1024 // default gRPC ConnWindowSize
)

// Config for the ADS connection.
type Config struct {
	// Namespace defaults to 'default'
	Namespace string

	// Workload defaults to 'test'
	Workload string

	// Revision for this control plane instance. We will only read configs that match this revision.
	Revision string

	// Meta includes additional metadata for the node
	Meta *pstruct.Struct

	Locality *core.Locality

	// NodeType defaults to sidecar. "ingress" and "router" are also supported.
	NodeType string

	// IP is currently the primary key used to locate inbound configs. It is sent by client,
	// must match a known endpoint IP. Tests can use a ServiceEntry to register fake IPs.
	IP string

	// CertDir is the directory where mTLS certs are configured.
	// If CertDir and Secret are empty, an insecure connection will be used.
	// TODO: implement SecretManager for cert dir
	CertDir string

	// Secrets is the interface used for getting keys and rootCA.
	SecretManager security.SecretManager

	// For getting the certificate, using same code as SDS server.
	// Either the JWTPath or the certs must be present.
	JWTPath string

	// XDSSAN is the expected SAN of the XDS server. If not set, the ProxyConfig.DiscoveryAddress is used.
	XDSSAN string

	// XDSRootCAFile explicitly set the root CA to be used for the XDS connection.
	// Mirrors Envoy file.
	XDSRootCAFile string

	// RootCert contains the XDS root certificate. Used mainly for tests, apps will normally use
	// XDSRootCAFile
	RootCert []byte

	// InsecureSkipVerify skips client verification the server's certificate chain and host name.
	InsecureSkipVerify bool

	// InitialDiscoveryRequests is a list of resources to watch at first, represented as URLs (for new XDS resource naming)
	// or type URLs.
	InitialDiscoveryRequests []*discovery.DiscoveryRequest

	// BackoffPolicy determines the reconnect policy. Based on MCP client.
	BackoffPolicy backoff.BackOff

	// ResponseHandler will be called on each DiscoveryResponse.
	// TODO: mirror Generator, allow adding handler per type
	ResponseHandler ResponseHandler

	GrpcOpts []grpc.DialOption
}

func DefaultGrpcDialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		// TODO(SpecialYang) maybe need to make it configurable.
		grpc.WithInitialWindowSize(int32(defaultInitialWindowSize)),
		grpc.WithInitialConnWindowSize(int32(defaultInitialConnWindowSize)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(defaultClientMaxReceiveMessageSize)),
	}
}

// ADSC implements a basic client for ADS, for use in stress tests and tools
// or libraries that need to connect to Istio pilot or other ADS servers.
type ADSC struct {
	// Stream is the GRPC connection stream, allowing direct GRPC send operations.
	// Set after Dial is called.
	stream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient
	// xds client used to create a stream
	client discovery.AggregatedDiscoveryServiceClient
	conn   *grpc.ClientConn

	// Indicates if the ADSC client is closed
	closed bool

	// NodeID is the node identity sent to Pilot.
	nodeID string

	url string

	watchTime time.Time

	// InitialLoad tracks the time to receive the initial configuration.
	InitialLoad time.Duration

	// httpListeners contains received listeners with a http_connection_manager filter.
	httpListeners map[string]*listener.Listener

	// tcpListeners contains all listeners of type TCP (not-HTTP)
	tcpListeners map[string]*listener.Listener

	// All received clusters of type eds, keyed by name
	edsClusters map[string]*cluster.Cluster

	// All received clusters of no-eds type, keyed by name
	clusters map[string]*cluster.Cluster

	// All received routes, keyed by route name
	routes map[string]*route.RouteConfiguration

	// All received endpoints, keyed by cluster name
	eds map[string]*endpoint.ClusterLoadAssignment

	// Metadata has the node metadata to send to pilot.
	// If nil, the defaults will be used.
	Metadata *pstruct.Struct

	// Updates includes the type of the last update received from the server.
	Updates     chan string
	errChan     chan error
	XDSUpdates  chan *discovery.DiscoveryResponse
	VersionInfo map[string]string

	// Last received message, by type
	Received map[string]*discovery.DiscoveryResponse

	mutex sync.RWMutex

	Mesh *v1alpha1.MeshConfig

	// Retrieved configurations can be stored using the common istio model interface.
	Store model.ConfigStore

	// Retrieved endpoints can be stored in the memory registry. This is used for CDS and EDS responses.
	Registry *memory.ServiceDiscovery

	// LocalCacheDir is set to a base name used to save fetched resources.
	// If set, each update will be saved.
	// TODO: also load at startup - so we can support warm up in init-container, and survive
	// restarts.
	LocalCacheDir string

	// RecvWg is for letting goroutines know when the goroutine handling the ADS stream finishes.
	RecvWg sync.WaitGroup

	cfg *Config

	// sendNodeMeta is set to true if the connection is new - and we need to send node meta.,
	sendNodeMeta bool

	sync     map[string]time.Time
	Locality *core.Locality
}

type ResponseHandler interface {
	HandleResponse(con *ADSC, response *discovery.DiscoveryResponse)
}

// jsonMarshalProtoWithName wraps a proto.Message with name so it can be marshaled with the standard encoding/json library
type jsonMarshalProtoWithName struct {
	Name    string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Message proto.Message
}

func (p jsonMarshalProtoWithName) MarshalJSON() ([]byte, error) {
	strSer, serr := protomarshal.ToJSONWithIndent(p.Message, "  ")
	if serr != nil {
		adscLog.Warnf("Error for marshaling [%s]: %v", p.Name, serr)
		return []byte(""), serr
	}
	serialItem := []byte("{\"" + p.Name + "\":" + strSer + "}")
	return serialItem, nil
}

var adscLog = log.RegisterScope("adsc", "adsc debugging", 0)

func NewWithBackoffPolicy(discoveryAddr string, opts *Config, backoffPolicy backoff.BackOff) (*ADSC, error) {
	adsc, err := New(discoveryAddr, opts)
	if err != nil {
		return nil, err
	}
	adsc.cfg.BackoffPolicy = backoffPolicy
	return adsc, err
}

// New creates a new ADSC, maintaining a connection to an XDS server.
// Will:
// - get certificate using the Secret provider, if CertRequired
// - connect to the XDS server specified in ProxyConfig
// - send initial request for watched resources
// - wait for response from XDS server
// - on success, start a background thread to maintain the connection, with exp. backoff.
func New(discoveryAddr string, opts *Config) (*ADSC, error) {
	if opts == nil {
		opts = &Config{}
	}
	// We want to recreate stream
	if opts.BackoffPolicy == nil {
		opts.BackoffPolicy = backoff.NewExponentialBackOff()
	}
	adsc := &ADSC{
		Updates:     make(chan string, 100),
		XDSUpdates:  make(chan *discovery.DiscoveryResponse, 100),
		VersionInfo: map[string]string{},
		url:         discoveryAddr,
		Received:    map[string]*discovery.DiscoveryResponse{},
		RecvWg:      sync.WaitGroup{},
		cfg:         opts,
		sync:        map[string]time.Time{},
		errChan:     make(chan error, 10),
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
	adsc.Locality = opts.Locality

	adsc.nodeID = fmt.Sprintf("%s~%s~%s.%s~%s.svc.%s", opts.NodeType, opts.IP,
		opts.Workload, opts.Namespace, opts.Namespace, constants.DefaultClusterLocalDomain)

	if err := adsc.Dial(); err != nil {
		return nil, err
	}

	return adsc, nil
}

// Dial connects to a ADS server, with optional MTLS authentication if a cert dir is specified.
func (a *ADSC) Dial() error {
	opts := a.cfg

	defaultGrpcDialOptions := DefaultGrpcDialOptions()
	var grpcDialOptions []grpc.DialOption
	grpcDialOptions = append(grpcDialOptions, defaultGrpcDialOptions...)
	grpcDialOptions = append(grpcDialOptions, opts.GrpcOpts...)

	var err error
	// If we need MTLS - CertDir or Secrets provider is set.
	if len(opts.CertDir) > 0 || opts.SecretManager != nil {
		tlsCfg, err := a.tlsConfig()
		if err != nil {
			return err
		}
		creds := credentials.NewTLS(tlsCfg)
		grpcDialOptions = append(grpcDialOptions, grpc.WithTransportCredentials(creds))
	}

	if len(grpcDialOptions) == len(defaultGrpcDialOptions) {
		// Only disable transport security if the user didn't supply custom dial options
		grpcDialOptions = append(grpcDialOptions, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	a.conn, err = grpc.Dial(a.url, grpcDialOptions...)
	if err != nil {
		return err
	}
	return nil
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

func (a *ADSC) tlsConfig() (*tls.Config, error) {
	var clientCerts []tls.Certificate
	var serverCABytes []byte
	var err error

	getClientCertificate := getClientCertFn(a.cfg)

	// Load the root CAs
	if a.cfg.RootCert != nil {
		serverCABytes = a.cfg.RootCert
	} else if a.cfg.XDSRootCAFile != "" {
		serverCABytes, err = os.ReadFile(a.cfg.XDSRootCAFile)
	} else if a.cfg.SecretManager != nil {
		// This is a bit crazy - we could just use the file
		rootCA, err := a.cfg.SecretManager.GenerateSecret(security.RootCertReqResourceName)
		if err != nil {
			return nil, err
		}

		serverCABytes = rootCA.RootCert
	} else if a.cfg.CertDir != "" {
		serverCABytes, err = os.ReadFile(a.cfg.CertDir + "/root-cert.pem")
		if err != nil {
			return nil, err
		}
	}

	serverCAs := x509.NewCertPool()
	if ok := serverCAs.AppendCertsFromPEM(serverCABytes); !ok {
		return nil, err
	}

	shost, _, _ := net.SplitHostPort(a.url)
	if a.cfg.XDSSAN != "" {
		shost = a.cfg.XDSSAN
	}

	return &tls.Config{
		GetClientCertificate: getClientCertificate,
		Certificates:         clientCerts,
		RootCAs:              serverCAs,
		ServerName:           shost,
		InsecureSkipVerify:   a.cfg.InsecureSkipVerify,
	}, nil
}

// Close the stream.
func (a *ADSC) Close() {
	a.mutex.Lock()
	_ = a.conn.Close()
	a.closed = true
	a.mutex.Unlock()
}

// Run will create a new stream using the existing grpc client connection and send the initial xds requests.
// And then it will run a go routine receiving and handling xds response.
// Note: it is non blocking
func (a *ADSC) Run() error {
	var err error
	a.client = discovery.NewAggregatedDiscoveryServiceClient(a.conn)
	a.stream, err = a.client.StreamAggregatedResources(context.Background())
	if err != nil {
		return err
	}
	a.sendNodeMeta = true
	a.InitialLoad = 0
	// Send the initial requests
	for _, r := range a.cfg.InitialDiscoveryRequests {
		if r.TypeUrl == v3.ClusterType {
			a.watchTime = time.Now()
		}
		_ = a.Send(r)
	}
	// by default, we assume 1 goroutine decrements the waitgroup (go a.handleRecv()).
	// for synchronizing when the goroutine finishes reading from the gRPC stream.

	a.RecvWg.Add(1)

	go a.handleRecv()
	return nil
}

// HasSynced returns true if MCP configs have synced
func (a *ADSC) HasSynced() bool {
	if a.cfg == nil || len(a.cfg.InitialDiscoveryRequests) == 0 {
		return true
	}

	a.mutex.RLock()
	defer a.mutex.RUnlock()

	for _, req := range a.cfg.InitialDiscoveryRequests {
		if strings.Count(req.TypeUrl, "/") != 3 {
			continue
		}

		if _, ok := a.sync[req.TypeUrl]; !ok {
			return false
		}
	}

	return true
}

// reconnect will create a new stream
func (a *ADSC) reconnect() {
	a.mutex.RLock()
	if a.closed {
		a.mutex.RUnlock()
		return
	}
	a.mutex.RUnlock()

	err := a.Run()
	if err == nil {
		a.cfg.BackoffPolicy.Reset()
	} else {
		time.AfterFunc(a.cfg.BackoffPolicy.NextBackOff(), a.reconnect)
	}
}

func (a *ADSC) handleRecv() {
	for {
		var err error
		msg, err := a.stream.Recv()
		if err != nil {
			a.RecvWg.Done()
			adscLog.Infof("Connection closed for node %v with err: %v", a.nodeID, err)
			select {
			case a.errChan <- err:
			default:
			}
			// if 'reconnect' enabled - schedule a new Run
			if a.cfg.BackoffPolicy != nil {
				time.AfterFunc(a.cfg.BackoffPolicy.NextBackOff(), a.reconnect)
			} else {
				a.Close()
				a.WaitClear()
				a.Updates <- ""
				a.XDSUpdates <- nil
				close(a.errChan)
			}
			return
		}

		// Group-value-kind - used for high level api generator.
		gvk := strings.SplitN(msg.TypeUrl, "/", 3)

		adscLog.Info("Received ", a.url, " type ", msg.TypeUrl,
			" cnt=", len(msg.Resources), " nonce=", msg.Nonce)
		if a.cfg.ResponseHandler != nil {
			a.cfg.ResponseHandler.HandleResponse(a, msg)
		}

		if msg.TypeUrl == collections.IstioMeshV1Alpha1MeshConfig.Resource().GroupVersionKind().String() &&
			len(msg.Resources) > 0 {
			rsc := msg.Resources[0]
			m := &v1alpha1.MeshConfig{}
			err = proto.Unmarshal(rsc.Value, m)
			if err != nil {
				adscLog.Warn("Failed to unmarshal mesh config", err)
			}
			a.Mesh = m
			if a.LocalCacheDir != "" {
				strResponse, err := protomarshal.ToJSONWithIndent(m, "  ")
				if err != nil {
					continue
				}
				err = os.WriteFile(a.LocalCacheDir+"_mesh.json", []byte(strResponse), 0o644)
				if err != nil {
					continue
				}
			}
			continue
		}

		// Process the resources.
		a.VersionInfo[msg.TypeUrl] = msg.VersionInfo
		switch msg.TypeUrl {
		case v3.ListenerType:
			listeners := make([]*listener.Listener, 0, len(msg.Resources))
			for _, rsc := range msg.Resources {
				valBytes := rsc.Value
				ll := &listener.Listener{}
				_ = proto.Unmarshal(valBytes, ll)
				listeners = append(listeners, ll)
			}
			a.handleLDS(listeners)
		case v3.ClusterType:
			clusters := make([]*cluster.Cluster, 0, len(msg.Resources))
			for _, rsc := range msg.Resources {
				valBytes := rsc.Value
				cl := &cluster.Cluster{}
				_ = proto.Unmarshal(valBytes, cl)
				clusters = append(clusters, cl)
			}
			a.handleCDS(clusters)
		case v3.EndpointType:
			eds := make([]*endpoint.ClusterLoadAssignment, 0, len(msg.Resources))
			for _, rsc := range msg.Resources {
				valBytes := rsc.Value
				el := &endpoint.ClusterLoadAssignment{}
				_ = proto.Unmarshal(valBytes, el)
				eds = append(eds, el)
			}
			a.handleEDS(eds)
		case v3.RouteType:
			routes := make([]*route.RouteConfiguration, 0, len(msg.Resources))
			for _, rsc := range msg.Resources {
				valBytes := rsc.Value
				rl := &route.RouteConfiguration{}
				_ = proto.Unmarshal(valBytes, rl)
				routes = append(routes, rl)
			}
			a.handleRDS(routes)
		default:
			a.handleMCP(gvk, msg.Resources)
		}

		// If we got no resource - still save to the store with empty name/namespace, to notify sync
		// This scheme also allows us to chunk large responses !

		// TODO: add hook to inject nacks

		a.mutex.Lock()
		if len(gvk) == 3 {
			gt := config.GroupVersionKind{Group: gvk[0], Version: gvk[1], Kind: gvk[2]}
			if _, exist := a.sync[gt.String()]; !exist {
				a.sync[gt.String()] = time.Now()
			}
		}
		a.Received[msg.TypeUrl] = msg
		a.ack(msg)
		a.mutex.Unlock()

		select {
		case a.XDSUpdates <- msg:
		default:
		}
	}
}

func (a *ADSC) mcpToPilot(m *mcp.Resource) (*config.Config, error) {
	if m == nil || m.Metadata == nil {
		return &config.Config{}, nil
	}
	c := &config.Config{
		Meta: config.Meta{
			ResourceVersion: m.Metadata.Version,
			Labels:          m.Metadata.Labels,
			Annotations:     m.Metadata.Annotations,
		},
	}

	if !config.ObjectInRevision(c, a.cfg.Revision) { // In case upstream does not support rev in node meta.
		return nil, nil
	}

	if c.Meta.Annotations == nil {
		c.Meta.Annotations = make(map[string]string)
	}
	nsn := strings.Split(m.Metadata.Name, "/")
	if len(nsn) != 2 {
		return nil, fmt.Errorf("invalid name %s", m.Metadata.Name)
	}
	c.Namespace = nsn[0]
	c.Name = nsn[1]
	var err error
	c.CreationTimestamp = m.Metadata.CreateTime.AsTime()

	pb, err := m.Body.UnmarshalNew()
	if err != nil {
		return nil, err
	}
	c.Spec = pb
	return c, nil
}

// nolint: staticcheck
func (a *ADSC) handleLDS(ll []*listener.Listener) {
	lh := map[string]*listener.Listener{}
	lt := map[string]*listener.Listener{}

	routes := []string{}
	ldsSize := 0

	for _, l := range ll {
		ldsSize += proto.Size(l)

		// The last filter is the actual destination for inbound listener
		if l.ApiListener != nil {
			// This is an API Listener
			// TODO: extract VIP and RDS or cluster
			continue
		}
		fc := l.FilterChains[len(l.FilterChains)-1]
		// Find the terminal filter
		filter := fc.Filters[len(fc.Filters)-1]

		// The actual destination will be the next to the last if the last filter is a passthrough filter
		if fc.GetName() == util.PassthroughFilterChain {
			fc = l.FilterChains[len(l.FilterChains)-2]
			filter = fc.Filters[len(fc.Filters)-1]
		}

		switch filter.Name {
		case wellknown.TCPProxy:
			lt[l.Name] = l
			config, _ := conversion.MessageToStruct(filter.GetTypedConfig())
			c := config.Fields["cluster"].GetStringValue()
			adscLog.Debugf("TCP: %s -> %s", l.Name, c)
		case wellknown.HTTPConnectionManager:
			lh[l.Name] = l

			// Getting from config is too painful..
			port := l.Address.GetSocketAddress().GetPortValue()
			if port == 15002 {
				routes = append(routes, "http_proxy")
			} else {
				routes = append(routes, fmt.Sprintf("%d", port))
			}
		case wellknown.MongoProxy:
			// ignore for now
		case wellknown.RedisProxy:
			// ignore for now
		case wellknown.MySQLProxy:
			// ignore for now
		default:
			adscLog.Infof(protomarshal.ToJSONWithIndent(l, "  "))
		}
	}

	adscLog.Infof("LDS: http=%d tcp=%d size=%d", len(lh), len(lt), ldsSize)
	if adscLog.DebugEnabled() {
		b, _ := json.MarshalIndent(ll, " ", " ")
		adscLog.Debugf(string(b))
	}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if len(routes) > 0 {
		a.sendRsc(v3.RouteType, routes)
	}
	a.httpListeners = lh
	a.tcpListeners = lt

	select {
	case a.Updates <- v3.ListenerType:
	default:
	}
}

// Save will save the json configs to files, using the base directory
func (a *ADSC) Save(base string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	// guarantee the persistence order for each element in tcpListeners
	var sortTCPListeners []string
	for key := range a.tcpListeners {
		sortTCPListeners = append(sortTCPListeners, key)
	}
	sort.Strings(sortTCPListeners)
	arrTCPListenersJSONProto := make([]jsonMarshalProtoWithName, 0, len(sortTCPListeners))
	for _, element := range sortTCPListeners {
		sliceItem := &jsonMarshalProtoWithName{element, a.tcpListeners[element]}
		arrTCPListenersJSONProto = append(arrTCPListenersJSONProto, *sliceItem)
	}
	byteJSONResponse, err := json.MarshalIndent(arrTCPListenersJSONProto, "", "  ")
	if err != nil {
		adscLog.Warnf("Error for marshaling TCPListeners: %v", err)
	}
	err = os.WriteFile(base+"_lds_tcp.json", byteJSONResponse, 0o644)
	if err != nil {
		return err
	}

	// guarantee the persistence order for each element in httpListeners
	var sortHTTPListeners []string
	for key := range a.httpListeners {
		sortHTTPListeners = append(sortHTTPListeners, key)
	}
	sort.Strings(sortHTTPListeners)
	arrHTTPListenersJSONProto := make([]jsonMarshalProtoWithName, 0, len(sortHTTPListeners))
	for _, element := range sortHTTPListeners {
		sliceItem := &jsonMarshalProtoWithName{element, a.httpListeners[element]}
		arrHTTPListenersJSONProto = append(arrHTTPListenersJSONProto, *sliceItem)
	}
	byteJSONResponse, err = json.MarshalIndent(arrHTTPListenersJSONProto, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(base+"_lds_http.json", byteJSONResponse, 0o644)
	if err != nil {
		return err
	}

	// guarantee the persistence order for each element in routes
	var sortRoutes []string
	for key := range a.routes {
		sortRoutes = append(sortRoutes, key)
	}
	sort.Strings(sortRoutes)
	arrRoutesJSONProto := make([]jsonMarshalProtoWithName, 0, len(sortRoutes))
	for _, element := range sortRoutes {
		sliceItem := &jsonMarshalProtoWithName{element, a.routes[element]}
		arrRoutesJSONProto = append(arrRoutesJSONProto, *sliceItem)
	}
	byteJSONResponse, err = json.MarshalIndent(arrRoutesJSONProto, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(base+"_rds.json", byteJSONResponse, 0o644)
	if err != nil {
		return err
	}

	// guarantee the persistence order for each element in edsClusters
	var sortEdsClusters []string
	for key := range a.edsClusters {
		sortEdsClusters = append(sortEdsClusters, key)
	}
	sort.Strings(sortEdsClusters)
	arrEdsClustersJSONProto := make([]jsonMarshalProtoWithName, 0, len(sortEdsClusters))
	for _, element := range sortEdsClusters {
		sliceItem := &jsonMarshalProtoWithName{element, a.edsClusters[element]}
		arrEdsClustersJSONProto = append(arrEdsClustersJSONProto, *sliceItem)
	}
	byteJSONResponse, err = json.MarshalIndent(arrEdsClustersJSONProto, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(base+"_ecds.json", byteJSONResponse, 0o644)
	if err != nil {
		return err
	}

	// guarantee the persistence order for each element in clusters
	var sortClusters []string
	for key := range a.clusters {
		sortClusters = append(sortClusters, key)
	}
	sort.Strings(sortClusters)
	arrClustersJSONProto := make([]jsonMarshalProtoWithName, 0, len(sortClusters))
	for _, element := range sortClusters {
		sliceItem := &jsonMarshalProtoWithName{element, a.clusters[element]}
		arrClustersJSONProto = append(arrClustersJSONProto, *sliceItem)
	}
	byteJSONResponse, err = json.MarshalIndent(arrClustersJSONProto, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(base+"_cds.json", byteJSONResponse, 0o644)
	if err != nil {
		return err
	}

	// guarantee the persistence order for each element in eds
	var sortEds []string
	for key := range a.eds {
		sortEds = append(sortEds, key)
	}
	sort.Strings(sortEds)
	arrEdsJSONProto := make([]jsonMarshalProtoWithName, 0, len(sortEds))
	for _, element := range sortEds {
		sliceItem := &jsonMarshalProtoWithName{element, a.eds[element]}
		arrEdsJSONProto = append(arrEdsJSONProto, *sliceItem)
	}
	byteJSONResponse, err = json.MarshalIndent(arrEdsJSONProto, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(base+"_eds.json", byteJSONResponse, 0o644)
	if err != nil {
		return err
	}

	return err
}

func (a *ADSC) handleCDS(ll []*cluster.Cluster) {
	cn := make([]string, 0, len(ll))
	cdsSize := 0
	edscds := map[string]*cluster.Cluster{}
	cds := map[string]*cluster.Cluster{}
	for _, c := range ll {
		cdsSize += proto.Size(c)
		switch v := c.ClusterDiscoveryType.(type) {
		case *cluster.Cluster_Type:
			if v.Type != cluster.Cluster_EDS {
				cds[c.Name] = c
				continue
			}
		}
		cn = append(cn, c.Name)
		edscds[c.Name] = c
	}

	adscLog.Infof("CDS: %d size=%d", len(cn), cdsSize)

	if len(cn) > 0 {
		a.sendRsc(v3.EndpointType, cn)
	}
	if adscLog.DebugEnabled() {
		b, _ := json.MarshalIndent(ll, " ", " ")
		adscLog.Debugf(string(b))
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.edsClusters = edscds
	a.clusters = cds

	select {
	case a.Updates <- v3.ClusterType:
	default:
	}
}

func (a *ADSC) node() *core.Node {
	n := &core.Node{
		Id:       a.nodeID,
		Locality: a.Locality,
	}
	if a.Metadata == nil {
		n.Metadata = &pstruct.Struct{
			Fields: map[string]*pstruct.Value{
				"ISTIO_VERSION": {Kind: &pstruct.Value_StringValue{StringValue: "65536.65536.65536"}},
			},
		}
	} else {
		n.Metadata = a.Metadata
		if a.Metadata.Fields["ISTIO_VERSION"] == nil {
			a.Metadata.Fields["ISTIO_VERSION"] = &pstruct.Value{Kind: &pstruct.Value_StringValue{StringValue: "65536.65536.65536"}}
		}
	}
	return n
}

// Raw send of a request.
func (a *ADSC) Send(req *discovery.DiscoveryRequest) error {
	if a.sendNodeMeta {
		req.Node = a.node()
		a.sendNodeMeta = false
	}
	req.ResponseNonce = time.Now().String()
	if adscLog.DebugEnabled() {
		strReq, _ := protomarshal.ToJSONWithIndent(req, "  ")
		adscLog.Debugf("Sending Discovery Request to istiod: %s", strReq)
	}
	return a.stream.Send(req)
}

func (a *ADSC) handleEDS(eds []*endpoint.ClusterLoadAssignment) {
	la := map[string]*endpoint.ClusterLoadAssignment{}
	edsSize := 0
	ep := 0
	for _, cla := range eds {
		edsSize += proto.Size(cla)
		la[cla.ClusterName] = cla
		ep += len(cla.Endpoints)
	}

	adscLog.Infof("eds: %d size=%d ep=%d", len(eds), edsSize, ep)
	if adscLog.DebugEnabled() {
		b, _ := json.MarshalIndent(eds, " ", " ")
		adscLog.Debugf(string(b))
	}
	if a.InitialLoad == 0 {
		// first load - Envoy loads listeners after endpoints
		_ = a.stream.Send(&discovery.DiscoveryRequest{
			Node:    a.node(),
			TypeUrl: v3.ListenerType,
		})
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.eds = la

	select {
	case a.Updates <- v3.EndpointType:
	default:
	}
}

func (a *ADSC) handleRDS(configurations []*route.RouteConfiguration) {
	vh := 0
	rcount := 0
	size := 0

	rds := map[string]*route.RouteConfiguration{}

	for _, r := range configurations {
		for _, h := range r.VirtualHosts {
			vh++
			for _, rt := range h.Routes {
				rcount++
				// Example: match:<prefix:"/" > route:<cluster:"outbound|9154||load-se-154.local" ...
				adscLog.Debugf("Handle route %v, path %v, cluster %v", h.Name, rt.Match.PathSpecifier, rt.GetRoute().GetCluster())
			}
		}
		rds[r.Name] = r
		size += proto.Size(r)
	}
	if a.InitialLoad == 0 {
		a.InitialLoad = time.Since(a.watchTime)
		adscLog.Infof("RDS: %d size=%d vhosts=%d routes=%d time=%d", len(configurations), size, vh, rcount, a.InitialLoad)
	} else {
		adscLog.Infof("RDS: %d size=%d vhosts=%d routes=%d", len(configurations), size, vh, rcount)
	}

	if adscLog.DebugEnabled() {
		b, _ := json.MarshalIndent(configurations, " ", " ")
		adscLog.Debugf(string(b))
	}

	a.mutex.Lock()
	a.routes = rds
	a.mutex.Unlock()

	select {
	case a.Updates <- v3.RouteType:
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

// WaitSingle waits for a single resource, and fails if the rejected type is
// returned. We avoid rejecting all other types to avoid race conditions. For
// example, a test asserting an incremental update of EDS may fail if a previous
// push's RDS response comes in later. Instead, we can reject events coming
// before (ie CDS). The only real alternative is to wait which introduces its own
// issues.
func (a *ADSC) WaitSingle(to time.Duration, want string, reject string) error {
	t := time.NewTimer(to)
	for {
		select {
		case t := <-a.Updates:
			if t == "" {
				return fmt.Errorf("closed")
			}
			if t != want && t == reject {
				return fmt.Errorf("wanted update for %v got %v", want, t)
			}
			if t == want {
				return nil
			}
			continue
		case <-t.C:
			return fmt.Errorf("timeout, still waiting for update for %v", want)
		}
	}
}

// Wait for an updates for all the specified types
// If updates is empty, this will wait for any update
func (a *ADSC) Wait(to time.Duration, updates ...string) ([]string, error) {
	t := time.NewTimer(to)
	want := map[string]struct{}{}
	for _, update := range updates {
		want[update] = struct{}{}
	}
	got := make([]string, 0, len(updates))
	for {
		select {
		case toDelete := <-a.Updates:
			if toDelete == "" {
				return got, fmt.Errorf("closed")
			}
			delete(want, toDelete)
			got = append(got, toDelete)
			if len(want) == 0 {
				return got, nil
			}
		case <-t.C:
			return got, fmt.Errorf("timeout, still waiting for updates: %v", want)
		}
	}
}

// WaitVersion waits for a new or updated for a typeURL.
func (a *ADSC) WaitVersion(to time.Duration, typeURL, lastVersion string) (*discovery.DiscoveryResponse, error) {
	t := time.NewTimer(to)
	a.mutex.Lock()
	ex := a.Received[typeURL]
	a.mutex.Unlock()
	if ex != nil {
		if lastVersion == "" {
			return ex, nil
		}
		if lastVersion != ex.VersionInfo {
			return ex, nil
		}
	}

	for {
		select {
		case t := <-a.XDSUpdates:
			if t == nil {
				return nil, fmt.Errorf("closed")
			}
			if t.TypeUrl == typeURL {
				return t, nil
			}

		case <-t.C:
			return nil, fmt.Errorf("timeout, still waiting for updates: %v", typeURL)
		case err, ok := <-a.errChan:
			if ok {
				return nil, err
			}
			return nil, fmt.Errorf("connection closed")
		}
	}
}

// EndpointsJSON returns the endpoints, formatted as JSON, for debugging.
func (a *ADSC) EndpointsJSON() string {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	out, _ := json.MarshalIndent(a.eds, " ", " ")
	return string(out)
}

// Watch will start watching resources, starting with CDS. Based on the CDS response
// it will start watching RDS and LDS.
func (a *ADSC) Watch() {
	a.watchTime = time.Now()
	_ = a.stream.Send(&discovery.DiscoveryRequest{
		Node:    a.node(),
		TypeUrl: v3.ClusterType,
	})
}

func ConfigInitialRequests() []*discovery.DiscoveryRequest {
	out := make([]*discovery.DiscoveryRequest, 0, len(collections.Pilot.All())+1)
	out = append(out, &discovery.DiscoveryRequest{
		TypeUrl: collections.IstioMeshV1Alpha1MeshConfig.Resource().GroupVersionKind().String(),
	})
	for _, sch := range collections.Pilot.All() {
		out = append(out, &discovery.DiscoveryRequest{
			TypeUrl: sch.Resource().GroupVersionKind().String(),
		})
	}

	return out
}

// WatchConfig will use the new experimental API watching, similar with MCP.
func (a *ADSC) WatchConfig() {
	_ = a.stream.Send(&discovery.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node:          a.node(),
		TypeUrl:       collections.IstioMeshV1Alpha1MeshConfig.Resource().GroupVersionKind().String(),
	})

	for _, sch := range collections.Pilot.All() {
		_ = a.stream.Send(&discovery.DiscoveryRequest{
			ResponseNonce: time.Now().String(),
			Node:          a.node(),
			TypeUrl:       sch.Resource().GroupVersionKind().String(),
		})
	}
}

func (a *ADSC) sendRsc(typeurl string, rsc []string) {
	ex := a.Received[typeurl]
	version := ""
	nonce := ""
	if ex != nil {
		version = ex.VersionInfo
		nonce = ex.Nonce
	}
	_ = a.stream.Send(&discovery.DiscoveryRequest{
		ResponseNonce: nonce,
		VersionInfo:   version,
		Node:          a.node(),
		TypeUrl:       typeurl,
		ResourceNames: rsc,
	})
}

func (a *ADSC) ack(msg *discovery.DiscoveryResponse) {
	var resources []string
	if msg.TypeUrl == v3.EndpointType {
		for c := range a.edsClusters {
			resources = append(resources, c)
		}
	}
	if msg.TypeUrl == v3.RouteType {
		for r := range a.routes {
			resources = append(resources, r)
		}
	}

	_ = a.stream.Send(&discovery.DiscoveryRequest{
		ResponseNonce: msg.Nonce,
		TypeUrl:       msg.TypeUrl,
		Node:          a.node(),
		VersionInfo:   msg.VersionInfo,
		ResourceNames: resources,
	})
}

// GetHTTPListeners returns all the http listeners.
func (a *ADSC) GetHTTPListeners() map[string]*listener.Listener {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.httpListeners
}

// GetTCPListeners returns all the tcp listeners.
func (a *ADSC) GetTCPListeners() map[string]*listener.Listener {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.tcpListeners
}

// GetEdsClusters returns all the eds type clusters.
func (a *ADSC) GetEdsClusters() map[string]*cluster.Cluster {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.edsClusters
}

// GetClusters returns all the non-eds type clusters.
func (a *ADSC) GetClusters() map[string]*cluster.Cluster {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.clusters
}

// GetRoutes returns all the routes.
func (a *ADSC) GetRoutes() map[string]*route.RouteConfiguration {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.routes
}

// GetEndpoints returns all the routes.
func (a *ADSC) GetEndpoints() map[string]*endpoint.ClusterLoadAssignment {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.eds
}

func (a *ADSC) handleMCP(gvk []string, resources []*any.Any) {
	if len(gvk) != 3 {
		return // Not MCP
	}
	// Generic - fill up the store
	if a.Store == nil {
		return
	}

	groupVersionKind := config.GroupVersionKind{Group: gvk[0], Version: gvk[1], Kind: gvk[2]}
	existingConfigs, err := a.Store.List(groupVersionKind, "")
	if err != nil {
		adscLog.Warnf("Error listing existing configs %v", err)
		return
	}

	received := make(map[string]*config.Config)
	for _, rsc := range resources {
		m := &mcp.Resource{}
		err := rsc.UnmarshalTo(m)
		if err != nil {
			adscLog.Warnf("Error unmarshalling received MCP config %v", err)
			continue
		}
		newCfg, err := a.mcpToPilot(m)
		if err != nil {
			adscLog.Warn("Invalid data ", err, " ", string(rsc.Value))
			continue
		}
		if newCfg == nil {
			continue
		}
		received[newCfg.Namespace+"/"+newCfg.Name] = newCfg

		newCfg.GroupVersionKind = groupVersionKind
		oldCfg := a.Store.Get(newCfg.GroupVersionKind, newCfg.Name, newCfg.Namespace)

		if oldCfg == nil {
			if _, err = a.Store.Create(*newCfg); err != nil {
				adscLog.Warnf("Error adding a new resource to the store %v", err)
				continue
			}
		} else if oldCfg.ResourceVersion != newCfg.ResourceVersion || newCfg.ResourceVersion == "" {
			// update the store only when resource version differs or unset.
			newCfg.Annotations[mem.ResourceVersion] = newCfg.ResourceVersion
			newCfg.ResourceVersion = oldCfg.ResourceVersion
			if _, err = a.Store.Update(*newCfg); err != nil {
				adscLog.Warnf("Error updating an existing resource in the store %v", err)
				continue
			}
		}
		if a.LocalCacheDir != "" {
			strResponse, err := json.MarshalIndent(newCfg, "  ", "  ")
			if err != nil {
				adscLog.Warnf("Error marshaling received MCP config %v", err)
				continue
			}
			err = os.WriteFile(a.LocalCacheDir+"_res."+
				newCfg.GroupVersionKind.Kind+"."+newCfg.Namespace+"."+newCfg.Name+".json", strResponse, 0o644)
			if err != nil {
				adscLog.Warnf("Error writing received MCP config to local file %v", err)
			}
		}
	}

	// remove deleted resources from cache
	for _, config := range existingConfigs {
		if _, ok := received[config.Namespace+"/"+config.Name]; !ok {
			if err := a.Store.Delete(config.GroupVersionKind, config.Name, config.Namespace, nil); err != nil {
				adscLog.Warnf("Error deleting an outdated resource from the store %v", err)
				continue
			}
			if a.LocalCacheDir != "" {
				err = os.Remove(a.LocalCacheDir + "_res." +
					config.GroupVersionKind.Kind + "." + config.Namespace + "." + config.Name + ".json")
				if err != nil {
					adscLog.Warnf("Error deleting received MCP config to local file %v", err)
				}
			}
		}
	}
}
