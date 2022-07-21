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
	"reflect"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"golang.org/x/net/context"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/set"
	"istio.io/pkg/log"
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

// HandlerContext provides an event for a single resource, allowing handlers to react to it.
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

type Client struct {
	handlers map[string]interface{}
	// Map of resources key to its node
	tree           map[resourceKey]resourceNode
	xdsClient      discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesClient
	conn           *grpc.ClientConn
	initialWatches []resourceKey
}

func (c Client) trigger(url string, r *discovery.Resource, event Event) (*handlerContext, error) {
	res := newProto(url)
	if r != nil {
		if err := r.Resource.UnmarshalTo(res); err != nil {
			return nil, err
		}
	}
	ctx := &handlerContext{}
	handler, f := c.handlers[url]
	if !f {
		log.Warnf("ignoring unknown type %v", url)
		return &handlerContext{}, nil
	}
	reflect.ValueOf(handler).Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(res), reflect.ValueOf(event)})
	return ctx, nil
}

// getProtoMessageType returns the Go lang type of the proto with the specified name.
func newProto(tt string) proto.Message {
	name := protoreflect.FullName(strings.TrimPrefix(tt, "type.googleapis.com/"))
	t, err := protoregistry.GlobalTypes.FindMessageByName(name)
	if err != nil || t == nil {
		return nil
	}
	return t.New().Interface().(proto.Message)
}

func (c *Client) Run(ctx context.Context) error {
	addr := "localhost:15010"

	conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial context: %v", err)
	}

	xds := discovery.NewAggregatedDiscoveryServiceClient(conn)
	xdsClient, err := xds.DeltaAggregatedResources(ctx, grpc.MaxCallRecvMsgSize(math.MaxInt32))
	if err != nil {
		return fmt.Errorf("delta stream: %v", err)
	}
	c.xdsClient = xdsClient
	c.conn = conn
	go c.handleRecv()
	for _, w := range c.initialWatches {
		c.request(w)
	}
	return nil
}

type Option func(c *Client)

func New(opts ...Option) *Client {
	c := &Client{
		handlers: map[string]interface{}{},
		tree:     map[resourceKey]resourceNode{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func TypeName[T proto.Message]() string {
	ft := new(T)
	return "type.googleapis.com/" + string((*ft).ProtoReflect().Descriptor().FullName())
}

func Register[T proto.Message](f func(ctx HandlerContext, res T, event Event)) Option {
	return func(c *Client) {
		c.handlers[TypeName[T]()] = f
	}
}

func Watch[T proto.Message](resourceName string) Option {
	return func(c *Client) {
		if resourceName == "*" {
			// Normalize to allow both forms
			resourceName = ""
		}
		key := resourceKey{
			Name:    resourceName,
			TypeUrl: TypeName[T](),
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
	for _, r := range d.Resources {
		if d.TypeUrl != r.Resource.TypeUrl {
			log.Fatalf("mismatch of type url: %v vs %v", d.TypeUrl, r.Resource.TypeUrl)
		}
		ctx, err := c.trigger(d.TypeUrl, r, EventAdd)
		if err != nil {
			return err
		}
		parentKey := resourceKey{
			Name:    r.Name,
			TypeUrl: r.Resource.TypeUrl,
		}
		c.establishResource(parentKey)

		if ctx.nack != nil {
			rejects = append(rejects, ctx.nack)
			// On NACK, do not apply resource changes
			continue
		}

		remove, add := c.tree[parentKey].Children.Split(ctx.sub)
		for key := range add {
			log.Errorf("howardjohn: add %v", key)
			if _, f := allAdds[key.TypeUrl]; !f {
				allAdds[key.TypeUrl] = set.New[string]()
			}
			allAdds[key.TypeUrl].Insert(key.Name)
			c.relate(parentKey, key)
		}
		for key := range remove {
			log.Errorf("howardjohn: remove %v", key)
			if _, f := allRemoves[key.TypeUrl]; !f {
				allRemoves[key.TypeUrl] = set.New[string]()
			}
			allRemoves[key.TypeUrl].Insert(key.Name)
			c.unrelate(parentKey, key)
		}
	}
	for _, r := range d.RemovedResources {
		// TODO: we need to actually store the resources so we can pass them down in this case
		_, err := c.trigger(d.TypeUrl, nil, EventDelete)
		if err != nil {
			return err
		}
		// TODO: do we need ctx at all?

		key := resourceKey{
			Name:    r,
			TypeUrl: d.TypeUrl,
		}
		if _, f := allRemoves[key.TypeUrl]; !f {
			allRemoves[key.TypeUrl] = set.New[string]()
		}
		allRemoves[key.TypeUrl].Insert(key.Name)
		c.drop(key)
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
	if len(rejects) == 0 {
		return nil
	}
	var e []string
	for _, r := range rejects {
		e = append(e, r.Error())
	}
	return errors.New(strings.Join(e, "; "))
}

// establishResource sets up the relationship for a resource we received.
func (c Client) establishResource(key resourceKey) {
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
		// TODO: no fatal, this can be unwatched resource from server
		log.Fatalf("recieved unsubscribed resource: %v, %v", key, c.tree)
	}
}

func (c Client) relate(parent, child resourceKey) {
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

func (c Client) drop(parent resourceKey) {
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

func (c Client) unrelate(parent, child resourceKey) {
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

	// EventUpdate is sent when an object is modified
	// Captures the modified object
	EventUpdate

	// EventDelete is sent when an object is deleted
	// Captures the object at the last known state
	EventDelete
)

func (event Event) String() string {
	out := "unknown"
	switch event {
	case EventAdd:
		out = "add"
	case EventUpdate:
		out = "update"
	case EventDelete:
		out = "delete"
	}
	return out
}

func (c Client) dumpTree() string {
	sb := strings.Builder{}
	roots := make(keySet)
	for key := range c.tree {
		if len(c.tree[key].Parents) == 0 {
			roots.Insert(key)
		}
	}
	for key := range roots {
		c.dumpNode(&sb, key, "")
	}
	return sb.String()
}

func (c Client) dumpNode(sb *strings.Builder, key resourceKey, indent string) {
	sb.WriteString(indent + key.ShortName() + ":\n")
	if len(indent) > 10 {
		return
	}
	node := c.tree[key]
	for child := range node.Children {
		id := indent + "  "
		// Not sure what this is -- two different parents?
		//if _, f := child.Parents[node]; !f {
		//	id = indent + "**"
		//}
		c.dumpNode(sb, child, id)
	}
}

func (c Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

func (c Client) request(w resourceKey) {
	// TODO: we need nonce for things after the first
	c.send(w, "", nil)
}

func (c Client) send(w resourceKey, nonce string, err error) {
	req := &discovery.DeltaDiscoveryRequest{
		Node: &core.Node{
			Id: c.NodeID(),
		},
		TypeUrl:       w.TypeUrl,
		ResponseNonce: nonce,
	}

	if w.Name != "" && err == nil {
		req.ResourceNamesSubscribe = []string{w.Name}
	}
	if err != nil {
		req.ErrorDetail = &status.Status{Message: err.Error()}
	}
	c.xdsClient.Send(req)
}

func (c Client) NodeID() string {
	return fmt.Sprintf("%s~%s~%s.%s~%s.svc.cluster.local", "sidecar", "1.1.1.1",
		"test", "test", "test")
}

func (c Client) update(t string, sub, unsub set.Set[string], d *discovery.DeltaDiscoveryResponse) {
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
