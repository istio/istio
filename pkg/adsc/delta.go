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
	"errors"
	"fmt"
	"math"
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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"k8s.io/utils/set"

	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/backoff"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

var deltaLog = log.RegisterScope("deltaadsc", "delta adsc debugging")

type resourceKey struct {
	Name    string
	TypeURL string
}

func (k resourceKey) shortName() string {
	return v3.GetShortType(k.TypeURL) + "/" + k.Name
}

type keySet = sets.Set[resourceKey]

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
			TypeURL: typeURL,
		}
		h.sub.Insert(key)
	}
}

func (h *handlerContext) Reject(reason error) {
	h.nack = reason
}

// DeltaADSConfig for delta ADS connection.
type DeltaADSConfig struct {
	Config
	// EnableDeletedResourceCache controls whether to maintain a cache for deleted resources.
	// When set to true, the system keeps a cache of resources that have been removed, which can be useful
	// for tracking or auditing purposes. If set to false, the caching of deleted resources is disabled,
	// potentially reducing memory usage. By default, this field is set to false, meaning caching for deleted
	// resources is disabled unless explicitly turned on.
	EnableDeletedResourceCache bool
}

type HandlerFunc func(ctx HandlerContext, res proto.Message, event Event)

// Client is a stateful ADS (Aggregated Discovery Service) client designed to handle delta updates from an xDS server.
// Central to this client is a dynamic 'tree' of resources, representing the relationships and states of resources in the service mesh.
// The client's operation unfolds in the following steps:
//
//  1. Sending Initial Requests: The client initiates requests for resources it needs, as specified by the Watch function.
//     This step sets the stage for receiving relevant DeltaDiscoveryResponse from the server.
//
//  2. Processing DeltaDiscoveryResponses: Upon receiving a delta response, the client performs several key actions:
//     - Event Handling: Triggers specific handlers for each resource, as registed using Register function during client initialization.
//     - Tree Update: Modifies its 'tree' to reflect changes in resources, such as adding new resources,
//     updating relationships between parents and children, and removing or unlinking resources.
//     - Resource Caching: If 'EnableDeletedResourceCache' is enabled, newly added resources are cached.
//     This cache plays a critical role when resources are subsequently removed from the stream, allowing
//     the client to trigger delete events with the cached resource. In the absence of caching, delete
//     events carry only the resource's name.
//
//  3. State Synchronization: Post-processing the delta response, the client updates its internal state. This involves:
//     - Acknowledgements and Errors: Communicating acknowledgements or errors back to the server based on the
//     processing outcome. In cases of error or rejection, a Nack can be sent using HandlerContext.Reject.
//     - Dependency Updates: Triggering requests for dependent resources. These dependencies are established via
//     HandlerContext.RegisterDependency.
type Client struct {
	cfg      *DeltaADSConfig
	handlers map[string]HandlerFunc
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

	// resourceCache is used to cache the resource received from the delta stream,
	// this is only used for triggering the delete event when the resource is removed from the stream.
	// If cfg.
	resourceCache sync.Map

	// errChan is used to signal errors from the delta stream
	errChan chan error

	// closed is set to true when the client is closed
	closed bool
}

func (c *Client) trigger(ctx *handlerContext, url string, r *discovery.Resource, event Event) error {
	res := newProto(url)
	if r != nil && res != nil {
		if err := r.Resource.UnmarshalTo(res); err != nil {
			return err
		}
	}
	handler, f := c.handlers[url]
	if !f {
		deltaLog.Warnf("ignoring unknown type %v", url)
		return nil
	}
	handler(ctx, res, event)
	return nil
}

func (c *Client) callHandler(handler HandlerFunc, ctx *handlerContext, url string, r *discovery.Resource, event Event) error {
	var res proto.Message
	if r != nil {
		res = newProto(url)
		if err := r.Resource.UnmarshalTo(res); err != nil {
			return err
		}
	}
	handler(ctx, res, event)
	return nil
}

// getProtoMessageType returns the Golang type of the proto with the specified name.
func newProto(tt string) proto.Message {
	name := protoreflect.FullName(strings.TrimPrefix(tt, resource.APITypePrefix))
	t, err := protoregistry.GlobalTypes.FindMessageByName(name)
	if err != nil || t == nil {
		return nil
	}
	return t.New().Interface()
}

func (c *Client) Run(ctx context.Context) error {
	if err := c.Dial(); err != nil {
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

func (c *Client) Dial() error {
	conn, err := dialWithConfig(&c.cfg.Config)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

// reconnect will create a new stream
func (c *Client) reconnect() {
	c.mutex.RLock()
	if c.closed {
		c.mutex.RUnlock()
		return
	}
	c.mutex.RUnlock()

	err := c.Run(context.Background())
	if err != nil {
		time.AfterFunc(c.cfg.BackoffPolicy.NextBackOff(), c.reconnect)
	}
}

type Option func(c *Client)

func NewDelta(discoveryAddr string, config *DeltaADSConfig, opts ...Option) *Client {
	if config == nil {
		config = &DeltaADSConfig{}
	}
	config.Address = discoveryAddr
	config.Config = setDefaultConfig(&config.Config)
	c := &Client{
		cfg:             config,
		handlers:        map[string]HandlerFunc{},
		tree:            map[resourceKey]resourceNode{},
		errChan:         make(chan error, 10),
		deltaXDSUpdates: make(chan *discovery.DeltaDiscoveryResponse, 100),
		received:        map[string]*discovery.DeltaDiscoveryResponse{},
		mutex:           sync.RWMutex{},
		resourceCache:   sync.Map{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func NewDeltaWithBackoffPolicy(discoveryAddr string, config *DeltaADSConfig, backoffPolicy backoff.BackOff, opts ...Option) *Client {
	if config == nil {
		config = &DeltaADSConfig{}
	}
	delta := NewDelta(discoveryAddr, config, opts...)
	delta.cfg.BackoffPolicy = backoffPolicy
	return delta
}

func typeName[T proto.Message]() string {
	ft := new(T)
	return resource.APITypePrefix + string((*ft).ProtoReflect().Descriptor().FullName())
}

// Register registers a handler for a type which is reflected by the proto message.
func Register[T proto.Message](f func(ctx HandlerContext, res T, event Event)) Option {
	return func(c *Client) {
		c.handlers[typeName[T]()] = func(ctx HandlerContext, res proto.Message, event Event) {
			f(ctx, res.(T), event)
		}
	}
}

// Watch registers an initial watch for a type based on the type reflected by the proto message.
func Watch[T proto.Message](resourceName string) Option {
	return initWatch(typeName[T](), resourceName)
}

func initWatch(typeURL string, resourceName string) Option {
	return func(c *Client) {
		if resourceName == "*" {
			// Normalize to allow both forms
			resourceName = ""
		}
		key := resourceKey{
			Name:    resourceName,
			TypeURL: typeURL,
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
	// We connected, so reset the backoff
	if c.cfg.BackoffPolicy != nil {
		c.cfg.BackoffPolicy.Reset()
	}
	for {
		deltaLog.Infof("Start Recv for node %v", c.nodeID)
		msg, err := c.xdsClient.Recv()
		if err != nil {
			deltaLog.Infof("Connection closed for node %v with err: %v", c.nodeID, err)
			select {
			case c.errChan <- err:
			default:
			}
			// if 'reconnect' enabled - schedule a new Run
			if c.cfg.BackoffPolicy != nil {
				time.AfterFunc(c.cfg.BackoffPolicy.NextBackOff(), c.reconnect)
			} else {
				c.Close()
				c.WaitClear()
				c.deltaXDSUpdates <- nil
				close(c.errChan)
			}
			return
		}
		deltaLog.Infof("Received response: %s", msg.TypeUrl)
		if err := c.handleDeltaResponse(msg); err != nil {
			deltaLog.Infof("Handle response %s failed: %v", msg.TypeUrl, err)
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
		return nil
	}
	for _, r := range d.Resources {
		if d.TypeUrl != r.Resource.TypeUrl {
			deltaLog.Fatalf("Invalid response: mismatch of type url: %v vs %v", d.TypeUrl, r.Resource.TypeUrl)
		}
		err := c.trigger(ctx, d.TypeUrl, r, EventAdd)
		if err != nil {
			return err
		}
		parentKey := resourceKey{
			Name:    r.Name,
			TypeURL: r.Resource.TypeUrl,
		}
		c.establishResource(parentKey)
		if c.cfg.EnableDeletedResourceCache {
			c.resourceCache.Store(parentKey, r)
		}
		if ctx.nack != nil {
			rejects = append(rejects, ctx.nack)
			// On NACK, do not apply resource changes
			continue
		}

		remove, add := c.tree[parentKey].Children.Diff(ctx.sub)
		for _, key := range add {
			if _, f := allAdds[key.TypeURL]; !f {
				allAdds[key.TypeURL] = set.New[string]()
			}
			allAdds[key.TypeURL].Insert(key.Name)
			c.relate(parentKey, key)
		}
		for _, key := range remove {
			if _, f := allRemoves[key.TypeURL]; !f {
				allRemoves[key.TypeURL] = set.New[string]()
			}
			allRemoves[key.TypeURL].Insert(key.Name)
			c.unrelate(parentKey, key)
		}
	}
	for _, r := range d.RemovedResources {
		key := resourceKey{
			Name:    r,
			TypeURL: d.TypeUrl,
		}
		var cached *discovery.Resource
		if c.cfg.EnableDeletedResourceCache {
			value, ok := c.resourceCache.Load(key)
			if ok {
				cached = value.(*discovery.Resource)
			}
		} else {
			cached = &discovery.Resource{
				Name: r,
			}
		}
		err := c.trigger(ctx, d.TypeUrl, cached, EventDelete)
		if err != nil {
			return err
		}

		if _, f := allRemoves[key.TypeURL]; !f {
			allRemoves[key.TypeURL] = set.New[string]()
		}
		allRemoves[key.TypeURL].Insert(key.Name)
		c.drop(key)
		if c.cfg.EnableDeletedResourceCache {
			c.resourceCache.Delete(key)
		}
	}
	c.send(resourceKey{TypeURL: d.TypeUrl}, d.Nonce, joinError(rejects))
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

	wildcardKey := resourceKey{TypeURL: key.TypeURL}
	wildNode, wildFound := c.tree[wildcardKey]
	if wildFound {
		wildNode.Children.Insert(key)
		parentNode.Parents.Insert(wildcardKey)
	}

	if !f && !wildFound {
		// We are receiving an unwanted resource, silently ignore it.
		deltaLog.Debugf("Received unsubscribed resource: %v, %v", key, c.tree)
	}
}

func (c *Client) relate(parent, child resourceKey) {
	parentNode, f := c.tree[parent]
	if !f {
		deltaLog.Fatalf("Failed to relate resource: unknown parent: %v, %v", parent, c.tree)
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
		deltaLog.Fatalf("Failed to drop resource: unknown parent: %v, %v", parent, c.tree)
	}
	for p := range parentNode.Parents {
		c.unrelate(p, parent)
	}

	if _, f := c.tree[parent]; f {
		deltaLog.Fatalf("Failed to drop resource: unrelate should have handled this: %v", c.dumpTree())
	}
}

func (c *Client) unrelate(parent, child resourceKey) {
	parentNode, f := c.tree[parent]
	if !f {
		deltaLog.Fatalf("Failed to unrelate resource: unknown parent: %v, %v", parent, c.tree)
	}
	parentNode.Children.Delete(child)
	childNode, f := c.tree[child]
	if !f {
		deltaLog.Fatalf("Failed to unrelate resource: unknown child: %v, %v", parent, c.tree)
	}
	// We are already watching, just update
	childNode.Parents.Delete(parent)

	if len(childNode.Parents) == 0 {
		// Node fully removed
		deltaLog.Infof("Removed resource: %v", child.shortName())
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
		return strings.Compare(a.shortName(), b.shortName())
	})
	for _, key := range keys {
		c.dumpNode(&sb, key, "")
	}
	return sb.String()
}

func (c *Client) dumpNode(sb *strings.Builder, key resourceKey, indent string) {
	sb.WriteString(indent + key.shortName() + ":\n")
	if len(indent) > 10 {
		return
	}
	node := c.tree[key]
	keys := slices.SortFunc(node.Children.UnsortedList(), func(a, b resourceKey) int {
		return strings.Compare(a.shortName(), b.shortName())
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
	c.mutex.Lock()
	c.conn.Close()
	c.closed = true
	c.mutex.Unlock()
}

func (c *Client) request(w resourceKey) {
	c.mutex.Lock()
	ex := c.received[w.TypeURL]
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
			Id: c.nodeID(),
		},
		TypeUrl:       w.TypeURL,
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
	err = c.xdsClient.Send(req)
	if err != nil {
		c.errChan <- err
	}
}

func (c *Client) nodeID() string {
	return nodeID(&c.cfg.Config)
}

func (c *Client) node() *core.Node {
	return buildNode(&c.cfg.Config)
}

func (c *Client) update(t string, sub, unsub set.Set[string], d *discovery.DeltaDiscoveryResponse) {
	req := &discovery.DeltaDiscoveryRequest{
		Node: &core.Node{
			Id: c.nodeID(),
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
	err := c.xdsClient.Send(req)
	if err != nil {
		c.errChan <- err
	}
}

// WaitClear will clear the waiting events, so next call to Wait will get
// the next push type.
func (c *Client) WaitClear() {
	for {
		select {
		case <-c.deltaXDSUpdates:
		default:
			return
		}
	}
}

// WaitResp waits for the latest delta response for a typeURL.
func (c *Client) WaitResp(to time.Duration, typeURL string) (*discovery.DeltaDiscoveryResponse, error) {
	t := time.NewTimer(to)
	c.mutex.Lock()
	ex := c.received[typeURL]
	c.mutex.Unlock()
	if ex != nil {
		return ex, nil
	}

	for {
		select {
		case t := <-c.deltaXDSUpdates:
			if t == nil {
				return nil, fmt.Errorf("closed")
			}
			if t.TypeUrl == typeURL {
				return t, nil
			}

		case <-t.C:
			return nil, fmt.Errorf("timeout, still waiting for updates: %v", typeURL)
		case err, ok := <-c.errChan:
			if ok {
				return nil, err
			}
			return nil, fmt.Errorf("connection closed")
		}
	}
}

func isDebugType(typeURL string) bool {
	return strings.HasPrefix(typeURL, v3.DebugType)
}
