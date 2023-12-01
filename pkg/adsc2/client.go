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
	"errors"
	"fmt"
	"math"
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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"k8s.io/utils/set"

	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type resourceKey struct {
	Name    string
	TypeURL string
}

func (k resourceKey) String() string {
	return k.TypeURL + "/" + k.Name
}

func (k resourceKey) ShortName() string {
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
	// TODO mcp, reconnect
	*adsc.Config
}

type Client struct {
	config   *DeltaADSConfig
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

	resourceCache sync.Map

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
	return t.New().Interface()
}

func (c *Client) Run(ctx context.Context) error {
	if err := c.dial(); err != nil {
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

func (c *Client) dial() error {
	conn, err := adsc.DialWithConfig(c.config.Config)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

type Option func(c *Client)

func New(config *DeltaADSConfig, opts ...Option) *Client {
	if config == nil {
		config = &DeltaADSConfig{}
	}
	config.Config = adsc.InitConfigIfNotPresent(config.Config)
	c := &Client{
		config:          config,
		handlers:        map[string]interface{}{},
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
	scope := log.WithLabels("node", c.NodeID())
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
			TypeURL: r.Resource.TypeUrl,
		}
		c.establishResource(parentKey)
		c.resourceCache.Store(parentKey, r)

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
		value, ok := c.resourceCache.Load(key)
		if ok {
			cached = value.(*discovery.Resource)
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
		c.resourceCache.Delete(key)
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
		log.Debugf("received unsubscribed resource: %v, %v", key, c.tree)
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
			Id: c.NodeID(),
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

func (c *Client) NodeID() string {
	return adsc.NodeID(c.config.Config)
}

func (c *Client) node() *core.Node {
	return adsc.BuildNode(c.config.Config)
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
	err := c.xdsClient.Send(req)
	if err != nil {
		c.errChan <- err
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
	return typeURL == xds.TypeDebugSyncronization || typeURL == xds.TypeDebugConfigDump || typeURL == xds.TypeURLConnect
}
