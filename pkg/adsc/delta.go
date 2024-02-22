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

// resourceNode represents a resource state in the dynamic tree structure of the service mesh.
// It tracks the relationships of a resource with its parents and children within the mesh.
//
// Example: Consider a scenario where we have a direct wildcard CDS watch.
// Upon receiving a response, suppose some CDS resources named A, B, etc., are added. The resulting tree structure would be:
//
//	CDS/*:
//	  CDS/A:
//	  CDS/B:
//
// In this case, CDS/A and CDS/B are nodes under the wildcard CDS watch.
//
// Further, if we register a dependency on an EDS resource named C for CDS added resources,
// the tree expands to:
//
//	CDS/*:
//	  CDS/A:
//	    EDS/C:
//	  CDS/B:
//	    EDS/C:
//
// Here, CDS/A and CDS/B become parents of EDS/C, and EDS/C is a child of both CDS/A and CDS/B.
//
// If a response later indicates that the CDS resource A is removed, all relationships originating from A are also removed.
// The updated tree would then be:
//
//	CDS/*:
//	  CDS/B:
//	    EDS/C:
//
// This change reflects the removal of CDS/A and its associated child link to EDS/C.
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
}

type Resource struct {
	Name    string
	Version string
	Entity  proto.Message
}

type HandlerFunc func(ctx HandlerContext, res *Resource, event Event)

// Client is a stateful ADS (Aggregated Discovery Service) client designed to handle delta updates from an xDS server.
// Central to this client is a dynamic 'tree' of resources, representing the relationships and states of resources in the service mesh.
// The client's operation unfolds in the following steps:
//
//  1. Sending Initial Requests: The client initiates requests for resources it needs, as specified by the Watch function.
//     This step sets the stage for receiving relevant DeltaDiscoveryResponse from the server.
//
//  2. Processing DeltaDiscoveryResponses: Upon receiving a delta response, the client performs several key actions:
//     - Event Handling: Triggers specific handlers for each resource, as register using Register function during client initialization.
//     - Tree Update: Modifies its 'tree' to reflect changes in resources, such as adding new resources,
//     updating relationships between parents and children, and removing or unlinking resources.
//
//  3. State Synchronization: Post-processing the delta response, the client updates its internal state. This involves:
//     - Acknowledgements and Errors: Communicating acknowledgements or errors back to the server based on the
//     processing outcome. In cases of error or rejection, a Nack can be sent using HandlerContext.Reject.
//     - Dependency Updates: Triggering requests for dependent resources. These dependencies are established via
//     HandlerContext.RegisterDependency.
//
// An example of a handler registration is as follows:
//
//	clusterHandler := Register(func(ctx HandlerContext, res *cluster.Cluster, event Event) {
//	  if event == EventDelete {
//	    return
//	  }
//	  ctx.RegisterDependency(v3.SecretType, ExtractClusterSecretResources(t, res)...)
//	  ctx.RegisterDependency(v3.EndpointType, ExtractEdsClusterNames([]*cluster.Cluster{res})...)
//	})
//
// It means that when a cluster is added or updated, the client will trigger requests for the
// secrets and endpoints that the cluster depends on.
//
// An example of register handlers:
//
//	handlers := []Option{
//	  clusterHandler,
//	  Watch[*cluster.Cluster]("*"),
//	  listenerHandler,
//	  Watch[*listener.Listener]("*"),
//	  endpointsHandler,
//	  routesHandler,
//	  secretsHandler,
//	}
//
// client := NewDelta("localhost:8080", handlers...)
//
// It means that the client will watch all clusters and listeners, and trigger resource events for
// clusters, listeners, endpoints, routes and secrets that the clusters and listeners depend on.
type Client struct {
	cfg      *DeltaADSConfig
	handlers map[string]HandlerFunc
	// tree is a map where each key is a `resourceKey` (comprising the resource name and typeURL)
	// and each added resource is a `resourceNode`. This tree structure represents the dynamic state
	// and relationships of resources.
	tree map[resourceKey]resourceNode

	xdsClient discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesClient
	conn      *grpc.ClientConn

	// initialWatches is the list of resources we are watching on startup
	initialWatches []resourceKey

	// sendNodeMeta is set to true if the connection is new - and we need to send node meta
	sendNodeMeta atomic.Bool

	mutex sync.RWMutex
	// lastReceived message, by type
	lastReceived    map[string]*discovery.DeltaDiscoveryResponse
	deltaXDSUpdates chan *discovery.DeltaDiscoveryResponse

	// errChan is used to signal errors from the delta stream
	errChan chan error

	// closed is set to true when the client is closed
	closed bool
}

func (c *Client) trigger(ctx *handlerContext, typeURL string, r *discovery.Resource, event Event) error {
	var res *Resource
	if r == nil {
		return fmt.Errorf("trigged by event %d,but resource is nil", event)
	}
	if event == EventAdd {
		if r.Resource == nil {
			return fmt.Errorf("trigged by EventAdd,but be added resource object is nil")
		}
		entity := newProto(typeURL)
		if entity == nil {
			return fmt.Errorf("new resource entity by typeURL: %s error", entity)
		}
		if err := r.Resource.UnmarshalTo(entity); err != nil {
			return err
		}
		res = &Resource{
			Name:    r.Name,
			Version: r.Version,
			Entity:  entity,
		}
	} else {
		// EventDelete
		res = &Resource{
			Name: r.Name,
		}
	}
	handler, f := c.handlers[typeURL]
	if !f {
		deltaLog.Warnf("ignoring unknown type %v", typeURL)
		return nil
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
	} else if c.cfg.BackoffPolicy != nil {
		// We connected, so reset the backoff
		c.cfg.BackoffPolicy.Reset()
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
		lastReceived:    map[string]*discovery.DeltaDiscoveryResponse{},
		mutex:           sync.RWMutex{},
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
func Register[T proto.Message](f func(ctx HandlerContext, resourceName string, resourceVersion string, resourceEntity T, event Event)) Option {
	return func(c *Client) {
		c.handlers[typeName[T]()] = func(ctx HandlerContext, res *Resource, event Event) {
			if res.Entity == nil {
				var nilEntity T
				f(ctx, res.Name, res.Version, nilEntity, event)
			} else {
				f(ctx, res.Name, res.Version, res.Entity.(T), event)
			}
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
			existing.Children = nil
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
			}
			return
		}
		deltaLog.Infof("Received response: %s", msg.TypeUrl)
		if err := c.handleDeltaResponse(msg); err != nil {
			deltaLog.Infof("Handle response %s failed: %v", msg.TypeUrl, err)
			c.Close()
			return
		}
		c.mutex.Lock()
		c.lastReceived[msg.TypeUrl] = msg
		c.mutex.Unlock()
		c.deltaXDSUpdates <- msg
	}
}

func (c *Client) handleDeltaResponse(d *discovery.DeltaDiscoveryResponse) error {
	var rejects []error
	allAdds := map[string]set.Set[string]{}
	allRemoves := map[string]set.Set[string]{}
	ctx := &handlerContext{}
	if isDebugType(d.TypeUrl) {
		// No need to ack and type check for debug types
		return nil
	}
	for _, r := range d.Resources {
		if d.TypeUrl != r.Resource.TypeUrl {
			deltaLog.Errorf("Invalid response: mismatch of type url: %v vs %v", d.TypeUrl, r.Resource.TypeUrl)
			continue
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
		removed := &discovery.Resource{
			Name: r,
		}
		err := c.trigger(ctx, d.TypeUrl, removed, EventDelete)
		if err != nil {
			return err
		}

		if _, f := allRemoves[key.TypeURL]; !f {
			allRemoves[key.TypeURL] = set.New[string]()
		}
		allRemoves[key.TypeURL].Insert(key.Name)
		c.drop(key)
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
	// Check if we have a watch for this resource
	parentNode, f := c.tree[key]
	if !f {
		parentNode = resourceNode{
			Parents:  make(keySet),
			Children: make(keySet),
		}
		c.tree[key] = parentNode
	}

	// Check if we have a Watch for all "*" resources, and if so, this specific resource is a child
	// of that watch.
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
	defer c.mutex.Unlock()
	c.conn.Close()
	c.closed = true
	c.WaitClear()
	// Signal the channel to close
	c.deltaXDSUpdates <- nil
}

func (c *Client) request(w resourceKey) {
	c.mutex.Lock()
	ex := c.lastReceived[w.TypeURL]
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
		case <-c.errChan:
		default:
			return
		}
	}
}

// WaitResp waits for the latest delta response for a typeURL.
func (c *Client) WaitResp(to time.Duration, typeURL string) (*discovery.DeltaDiscoveryResponse, error) {
	t := time.NewTimer(to)
	c.mutex.Lock()
	ex := c.lastReceived[typeURL]
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
