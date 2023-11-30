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

package adsc2

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"go.uber.org/atomic"
	"golang.org/x/net/context"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	pstruct "google.golang.org/protobuf/types/known/structpb"
	"istio.io/istio/pkg/slices"

	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/set"
)

type resourceKey struct {
	Name    string
	TypeUrl string
}

func (k resourceKey) String() string {
	return k.TypeUrl + "/" + k.Name
}

func (k resourceKey) ShortName() string {
	return v3.GetShortType(k.TypeUrl) + "/" + k.Name
}

type keySet = set.Set[resourceKey]

type resourceNode struct {
	// Parents of the resource. If nil, this is explicitly watched
	Parents keySet
	// Children of the resource
	Children keySet
}

type HandlerContext interface {
	RegisterDependency(typeURL string, resourceName ...string)
	Reject(reason error)
}

var _ HandlerContext = &handlerContext{}

// HandlerContext provides an event for a single delta response, allowing handlers to react to it.
// Operations done in the handler may be batched together with other handler's.
type handlerContext struct {
	sub  keySet
	nack error
}

func (h *handlerContext) RegisterDependency(typeURL string, resourceName ...string) {
	if h.sub == nil {
		h.sub = make(keySet)
	}
	for _, r := range resourceName {
		key := resourceKey{
			Name:    r,
			TypeUrl: typeURL,
		}
		h.sub.Insert(key)
	}
}

func (h *handlerContext) Reject(reason error) {
	h.nack = reason
}

type Config struct {
	// Address of the xDS server
	Address string
	// Namespace of the node, defaults to "default"
	Namespace string
	// Workload name of the node, defaults to "test"
	Workload string
	// NodeType defaults to sidecar. "ingress" and "router" are also supported.
	NodeType string
	// IP is currently the primary key used to locate inbound configs. It is sent by client,
	// must match a known endpoint IP. Tests can use a ServiceEntry to register fake IPs.
	IP string
	// proxy Meta includes additional metadata for the node
	Meta *pstruct.Struct
	// Locality of the node
	Locality *core.Locality
	// TODO revision

	GrpcOpts []grpc.DialOption

	// CertDir is the directory where mTLS certs are configured.
	// If CertDir and Secret are empty, an insecure connection will be used.
	CertDir string
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
	// Secrets is the interface used for getting keys and rootCA.
	SecretManager security.SecretManager
}

func initConfigIfNotPresent(config *Config) *Config {
	if config == nil {
		config = &Config{}
	}
	if config.Namespace == "" {
		config.Namespace = "default"
	}
	if config.NodeType == "" {
		config.NodeType = "sidecar"
	}
	if config.IP == "" {
		config.IP = adsc.GetPrivateIPIfAvailable().String()
	}
	if config.Workload == "" {
		config.Workload = "test"
	}
	return config
}

type Client struct {
	config   *Config
	handlers map[string]interface{}
	// Map of resources key to its node
	tree map[resourceKey]resourceNode

	xdsClient discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesClient
	conn      *grpc.ClientConn

	// initialWatches is the list of resources we are watching on startup
	initialWatches []resourceKey

	// sendNodeMeta is set to true if the connection is new - and we need to send node meta
	sendNodeMeta atomic.Bool

	mutex sync.RWMutex
	// Last received message, by type
	received        map[string]*discovery.DeltaDiscoveryResponse
	deltaXDSUpdates chan *discovery.DeltaDiscoveryResponse

	resourceCache *cache

	errChan chan error
}

func (c *Client) trigger(ctx *handlerContext, url string, r *discovery.Resource, event Event) error {
	var res proto.Message
	if isDebugType(url) {
		res = r.Resource
	} else {
		res = newProto(url)
		if r != nil && res != nil {
			if err := r.Resource.UnmarshalTo(res); err != nil {
				return err
			}
		}
	}
	handler, f := c.handlers[url]
	if !f {
		log.Warnf("ignoring unknown type %v", url)
		return nil
	}
	reflect.ValueOf(handler).Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(res), reflect.ValueOf(event)})
	return nil
}

// getProtoMessageType returns the Golang type of the proto with the specified name.
func newProto(tt string) proto.Message {
	name := protoreflect.FullName(strings.TrimPrefix(tt, resource.APITypePrefix))
	t, err := protoregistry.GlobalTypes.FindMessageByName(name)
	if err != nil || t == nil {
		return nil
	}
	return t.New().Interface().(proto.Message)
}

func (c *Client) Run(ctx context.Context) error {
	if err := c.dial(ctx); err != nil {
		return fmt.Errorf("dial context: %v", err)
	}

	xds := discovery.NewAggregatedDiscoveryServiceClient(c.conn)
	xdsClient, err := xds.DeltaAggregatedResources(ctx, grpc.MaxCallRecvMsgSize(math.MaxInt32))
	if err != nil {
		return fmt.Errorf("delta stream: %v", err)
	}
	c.sendNodeMeta.Store(true)
	c.xdsClient = xdsClient
	go c.handleRecv()
	for _, w := range c.initialWatches {
		c.request(w)
	}
	return nil
}

func (c *Client) dial(ctx context.Context) error {
	defaultOptions := adsc.DefaultGrpcDialOptions()
	opts := append(defaultOptions, c.config.GrpcOpts...)

	var err error
	// If we need MTLS - CertDir or Secrets provider is set.
	if len(c.config.CertDir) > 0 || c.config.SecretManager != nil {
		tlsCfg, err := c.tlsConfig()
		if err != nil {
			return err
		}
		creds := credentials.NewTLS(tlsCfg)
		opts = append(opts, grpc.WithTransportCredentials(creds))
	}

	// Only disable transport security if the user didn't supply custom dial options
	if len(opts) == len(defaultOptions) {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.DialContext(ctx, c.config.Address, opts...)
	if err != nil {
		return fmt.Errorf("dial context: %v", err)
	}
	c.conn = conn
	return nil
}

func (c *Client) tlsConfig() (*tls.Config, error) {
	var clientCerts []tls.Certificate
	var serverCABytes []byte
	var err error

	getClientCertificate := getClientCertFn(c.config)

	// Load the root CAs
	if c.config.RootCert != nil {
		serverCABytes = c.config.RootCert
	} else if c.config.XDSRootCAFile != "" {
		serverCABytes, err = os.ReadFile(c.config.XDSRootCAFile)
		if err != nil {
			return nil, err
		}
	} else if c.config.SecretManager != nil {
		// This is a bit crazy - we could just use the file
		rootCA, err := c.config.SecretManager.GenerateSecret(security.RootCertReqResourceName)
		if err != nil {
			return nil, err
		}

		serverCABytes = rootCA.RootCert
	} else if c.config.CertDir != "" {
		serverCABytes, err = os.ReadFile(c.config.CertDir + "/root-cert.pem")
		if err != nil {
			return nil, err
		}
	}

	serverCAs := x509.NewCertPool()
	if ok := serverCAs.AppendCertsFromPEM(serverCABytes); !ok {
		return nil, err
	}

	shost, _, _ := net.SplitHostPort(c.config.Address)
	if c.config.XDSSAN != "" {
		shost = c.config.XDSSAN
	}

	// nolint: gosec
	// it's insecure only when a user explicitly enable insecure mode.
	return &tls.Config{
		GetClientCertificate: getClientCertificate,
		Certificates:         clientCerts,
		RootCAs:              serverCAs,
		ServerName:           shost,
		InsecureSkipVerify:   c.config.InsecureSkipVerify,
	}, nil
}

type Option func(c *Client)

func New(config *Config, opts ...Option) *Client {
	c := &Client{
		config:          initConfigIfNotPresent(config),
		handlers:        map[string]interface{}{},
		tree:            map[resourceKey]resourceNode{},
		errChan:         make(chan error, 10),
		deltaXDSUpdates: make(chan *discovery.DeltaDiscoveryResponse, 100),
		received:        map[string]*discovery.DeltaDiscoveryResponse{},
		mutex:           sync.RWMutex{},
		resourceCache:   newResourceCache(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func TypeName[T proto.Message]() string {
	ft := new(T)
	return resource.APITypePrefix + string((*ft).ProtoReflect().Descriptor().FullName())
}

// Register registers a handler for a type which is reflected by the proto message.
func Register[T proto.Message](f func(ctx HandlerContext, res T, event Event)) Option {
	return func(c *Client) {
		c.handlers[TypeName[T]()] = f
	}
}

// Watch registers an initial watch for a type based on the type reflected by the proto message.
func Watch[T proto.Message](resourceName string) Option {
	return initWatch(TypeName[T](), resourceName)
}

// WatchType registers an initial watch for a type based on the type URL.
func WatchType(typeURL string, resourceName string) Option {
	return initWatch(typeURL, resourceName)
}

func initWatch(typeURL string, resourceName string) Option {
	return func(c *Client) {
		if resourceName == "*" {
			// Normalize to allow both forms
			resourceName = ""
		}
		key := resourceKey{
			Name:    resourceName,
			TypeUrl: typeURL,
		}
		existing, f := c.tree[key]
		if f {
			// We are watching directly now, so erase any parents
			existing.Parents = nil
		} else {
			c.tree[key] = resourceNode{
				Parents:  make(keySet),
				Children: make(keySet),
			}
		}
		c.initialWatches = append(c.initialWatches, key)
	}
}

func (c *Client) handleRecv() {
	scope := log.WithLabels("node", c.NodeID())
	// TODO reconnect
	for {
		scope.Infof("start Recv")
		msg, err := c.xdsClient.Recv()
		if err != nil {
			scope.Infof("Connection closed: %v", err)
			c.errChan <- err
			c.Close()
			return
		}
		scope.Infof("Received response: %v", msg.TypeUrl)
		if err := c.handleDeltaResponse(msg); err != nil {
			scope.Infof("handle failed: %v", err)
			c.Close()
			return
		}
	}
}

func (c *Client) handleDeltaResponse(d *discovery.DeltaDiscoveryResponse) error {
	var rejects []error
	allAdds := map[string]set.Set[string]{}
	allRemoves := map[string]set.Set[string]{}
	ctx := &handlerContext{}
	defer func() {
		c.mutex.Lock()
		c.received[d.TypeUrl] = d
		c.mutex.Unlock()
		select {
		case c.deltaXDSUpdates <- d:
		default:
		}
	}()
	if isDebugType(d.TypeUrl) {
		// No need to ack and type check for debug types
		// TODO do we need to trigger handlers for debug types?
		for _, r := range d.Resources {
			err := c.trigger(ctx, d.TypeUrl, r, EventAdd)
			if err != nil {
				return err
			}
		}
		return nil
	}
	for _, r := range d.Resources {
		if d.TypeUrl != r.Resource.TypeUrl {
			log.Fatalf("mismatch of type url: %v vs %v", d.TypeUrl, r.Resource.TypeUrl)
		}
		err := c.trigger(ctx, d.TypeUrl, r, EventAdd)
		if err != nil {
			return err
		}
		parentKey := resourceKey{
			Name:    r.Name,
			TypeUrl: r.Resource.TypeUrl,
		}
		c.establishResource(parentKey)
		c.resourceCache.put(parentKey, r)

		if ctx.nack != nil {
			rejects = append(rejects, ctx.nack)
			// On NACK, do not apply resource changes
			continue
		}

		remove, add := c.tree[parentKey].Children.Split(ctx.sub)
		for key := range add {
			if _, f := allAdds[key.TypeUrl]; !f {
				allAdds[key.TypeUrl] = set.New[string]()
			}
			allAdds[key.TypeUrl].Insert(key.Name)
			c.relate(parentKey, key)
		}
		for key := range remove {
			if _, f := allRemoves[key.TypeUrl]; !f {
				allRemoves[key.TypeUrl] = set.New[string]()
			}
			allRemoves[key.TypeUrl].Insert(key.Name)
			c.unrelate(parentKey, key)
		}
	}
	for _, r := range d.RemovedResources {
		key := resourceKey{
			Name:    r,
			TypeUrl: d.TypeUrl,
		}
		cached := c.resourceCache.get(key)
		err := c.trigger(ctx, d.TypeUrl, cached, EventDelete)
		if err != nil {
			return err
		}

		if _, f := allRemoves[key.TypeUrl]; !f {
			allRemoves[key.TypeUrl] = set.New[string]()
		}
		allRemoves[key.TypeUrl].Insert(key.Name)
		c.drop(key)
		c.resourceCache.delete(key)
	}
	c.send(resourceKey{TypeUrl: d.TypeUrl}, d.Nonce, joinError(rejects))
	for t, sub := range allAdds {
		unsub, f := allRemoves[t]
		if f {
			delete(allRemoves, t)
		}
		c.update(t, sub, unsub, d)
	}
	return nil
}

func joinError(rejects []error) error {
	var e []string
	for _, r := range rejects {
		if r == nil {
			continue
		}
		e = append(e, r.Error())
	}
	if len(e) == 0 {
		return nil
	}
	return errors.New(strings.Join(e, "; "))
}

// establishResource sets up the relationship for a resource we received.
func (c *Client) establishResource(key resourceKey) {
	parentNode, f := c.tree[key]
	if !f {
		parentNode = resourceNode{
			Parents:  make(keySet),
			Children: make(keySet),
		}
		c.tree[key] = parentNode
	}

	wildcardKey := resourceKey{TypeUrl: key.TypeUrl}
	wildNode, wildFound := c.tree[wildcardKey]
	if wildFound {
		wildNode.Children.Insert(key)
		parentNode.Parents.Insert(wildcardKey)
	}

	if !f && !wildFound {
		// We are receiving an unwanted resource, silently ignore it.
		log.Debugf("recieved unsubscribed resource: %v, %v", key, c.tree)
	}
}

func (c *Client) relate(parent, child resourceKey) {
	parentNode, f := c.tree[parent]
	if !f {
		log.Fatalf("unknown parent: %v, %v", parent, c.tree)
	}
	childNode, f := c.tree[child]
	if !f {
		// Not yet watching child, create a node
		c.tree[child] = resourceNode{
			Parents:  make(keySet),
			Children: make(keySet),
		}
		childNode = c.tree[child]
	}
	// We are already watching, just update
	childNode.Parents.Insert(parent)
	parentNode.Children.Insert(child)
}

func (c *Client) drop(parent resourceKey) {
	parentNode, f := c.tree[parent]
	if !f {
		log.Fatalf("unknown parent: %v, %v", parent, c.tree)
	}
	for p := range parentNode.Parents {
		c.unrelate(p, parent)
	}

	if _, f := c.tree[parent]; f {
		log.Fatalf("unrelate should have handled this: %v", c.dumpTree())
	}
}

func (c *Client) unrelate(parent, child resourceKey) {
	parentNode, f := c.tree[parent]
	if !f {
		log.Fatalf("unknown parent: %v, %v", parent, c.tree)
	}
	parentNode.Children.Delete(child)
	childNode, f := c.tree[child]
	if !f {
		log.Fatalf("unknown child: %v, %v", parent, c.tree)
	}
	// We are already watching, just update
	childNode.Parents.Delete(parent)

	if len(childNode.Parents) == 0 {
		// Node fully removed
		log.Infof("forgetting %v", child.ShortName())
		delete(c.tree, child)
	}
}

// Event represents a registry update event
type Event int

const (
	// EventAdd is sent when an object is added
	EventAdd Event = iota

	// EventDelete is sent when an object is deleted
	// Captures the object at the last known state
	EventDelete
)

func (event Event) String() string {
	out := "unknown"
	switch event {
	case EventAdd:
		out = "add"
	case EventDelete:
		out = "delete"
	}
	return out
}

func (c *Client) dumpTree() string {
	sb := strings.Builder{}
	roots := make(keySet)
	for key := range c.tree {
		if len(c.tree[key].Parents) == 0 {
			roots.Insert(key)
		}
	}
	keys := slices.SortFunc(roots.UnsortedList(), func(a, b resourceKey) int {
		return strings.Compare(a.ShortName(), b.ShortName())
	})
	for _, key := range keys {
		c.dumpNode(&sb, key, "")
	}
	return sb.String()
}

func (c *Client) dumpNode(sb *strings.Builder, key resourceKey, indent string) {
	sb.WriteString(indent + key.ShortName() + ":\n")
	if len(indent) > 10 {
		return
	}
	node := c.tree[key]
	keys := slices.SortFunc(node.Children.UnsortedList(), func(a, b resourceKey) int {
		return strings.Compare(a.ShortName(), b.ShortName())
	})
	for _, child := range keys {
		id := indent + "  "
		// Not sure what this is -- two different parents?
		//if _, f := child.Parents[node]; !f {
		//	id = indent + "**"
		//}
		c.dumpNode(sb, child, id)
	}
}

func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) request(w resourceKey) {
	c.mutex.Lock()
	ex := c.received[w.TypeUrl]
	c.mutex.Unlock()
	nonce := ""
	if ex != nil {
		nonce = ex.Nonce
	}
	c.send(w, nonce, nil)
}

func (c *Client) send(w resourceKey, nonce string, err error) {
	req := &discovery.DeltaDiscoveryRequest{
		Node: &core.Node{
			Id: c.NodeID(),
		},
		TypeUrl:       w.TypeUrl,
		ResponseNonce: nonce,
	}
	if c.sendNodeMeta.Load() {
		req.Node = c.node()
		c.sendNodeMeta.Store(false)
	}

	if w.Name != "" && err == nil {
		req.ResourceNamesSubscribe = []string{w.Name}
	}
	if err != nil {
		req.ErrorDetail = &status.Status{Message: err.Error()}
	}
	c.xdsClient.Send(req)
}

func (c *Client) NodeID() string {
	return fmt.Sprintf("%s~%s~%s.%s~%s.svc.%s", c.config.NodeType, c.config.IP,
		c.config.Workload, c.config.Namespace, c.config.Namespace, constants.DefaultClusterLocalDomain)
}

func (c *Client) node() *core.Node {
	n := &core.Node{
		Id:       c.NodeID(),
		Locality: c.config.Locality,
	}
	if c.config.Meta == nil {
		n.Metadata = &pstruct.Struct{
			Fields: map[string]*pstruct.Value{
				"ISTIO_VERSION": {Kind: &pstruct.Value_StringValue{StringValue: "65536.65536.65536"}},
			},
		}
	} else {
		n.Metadata = c.config.Meta
		if c.config.Meta.Fields["ISTIO_VERSION"] == nil {
			c.config.Meta.Fields["ISTIO_VERSION"] = &pstruct.Value{Kind: &pstruct.Value_StringValue{StringValue: "65536.65536.65536"}}
		}
	}
	return n
}

func (c *Client) update(t string, sub, unsub set.Set[string], d *discovery.DeltaDiscoveryResponse) {
	req := &discovery.DeltaDiscoveryRequest{
		Node: &core.Node{
			Id: c.NodeID(),
		},
		TypeUrl:       t,
		ResponseNonce: d.Nonce,
	}
	if sub != nil {
		req.ResourceNamesSubscribe = sub.UnsortedList()
	}
	if unsub != nil {
		req.ResourceNamesUnsubscribe = unsub.UnsortedList()
	}
	c.xdsClient.Send(req)
}

// WaitResources waits for a new or updated for a typeURL.
func (c *Client) WaitResources(to time.Duration, typeURL string) (added []*discovery.Resource, removed []*discovery.Resource, err error) {
	t := time.NewTimer(to)
	c.mutex.Lock()
	ex := c.received[typeURL]
	c.mutex.Unlock()
	if ex != nil {
		return ex.Resources, c.getResources(ex.TypeUrl, ex.RemovedResources...), nil
	}

	for {
		select {
		case t := <-c.deltaXDSUpdates:
			if t == nil {
				return nil, nil, fmt.Errorf("closed")
			}
			if t.TypeUrl == typeURL {
				return t.Resources, c.getResources(t.TypeUrl, t.RemovedResources...), nil
			}

		case <-t.C:
			return nil, nil, fmt.Errorf("timeout, still waiting for updates: %v", typeURL)
		case err, ok := <-c.errChan:
			if ok {
				return nil, nil, err
			}
			return nil, nil, fmt.Errorf("connection closed")
		}
	}
}

func (c *Client) getResources(typeURL string, resourceNames ...string) []*discovery.Resource {
	resources := make([]*discovery.Resource, 0, len(resourceNames))
	for _, name := range resourceNames {
		key := resourceKey{
			Name:    name,
			TypeUrl: typeURL,
		}
		cached := c.resourceCache.get(key)
		if cached != nil {
			resources = append(resources, cached)
		}
	}
	return resources
}
