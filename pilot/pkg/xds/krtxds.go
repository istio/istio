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

// Crediting the kgateway authors for the patterns used in this file, as well as some of the code

package xds

import (
	"fmt"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

const gatewayNameLabel = "gateway.networking.k8s.io/gateway-name"

// DiscoveryResource is the type for collections that will result in xDS config generation
// If ForGateway is nil, the resource is global and should be included in all gateways.
// If ForGateway is set to an empty NamespacedName, the collection is scoped but not this resource.
// If ForGateway is set to a specific NamespacedName, the resource is only relevant for that gateway.
type DiscoveryResource struct {
	*discovery.Resource
	ForGateway *types.NamespacedName
}

func (d DiscoveryResource) Equals(other DiscoveryResource) bool {
	return protoconv.Equals(d.Resource, other.Resource) && ptr.Equal(d.ForGateway, other.ForGateway)
}

func (d DiscoveryResource) IsForGateway(other types.NamespacedName) bool {
	// Not scoped || collection is scoped but this resource isn't || it is scoped to this one
	return d.ForGateway == nil || *d.ForGateway == (types.NamespacedName{}) || *d.ForGateway == other
}

func (d DiscoveryResource) ResourceName() string {
	if d.ForGateway != nil {
		return d.ForGateway.String() + "/" + d.Name
	}
	return d.Name
}

// CollectionGenerator generates discovery resources for a collection. It supports both per-gateway
// and global collections, and is designed to be efficient for delta updates.
type CollectionGenerator struct {
	PerGateway bool
	Col        krt.Collection[DiscoveryResource]
}

// CollectionRegistration defines the start and synced functions for a CollectionGenerator,
// which are used to manage the lifecycle of the collection in the context of xDS generation.
type CollectionRegistration struct {
	Start     func(stop <-chan struct{})
	HasSynced func() bool
}

// Registration defines the function to configure and synchronize krt collections per gateway or for all
// agentgateways. The function takes a map of collection generators indexed by typeUrl and a push channel,
// and returns a CollectionRegistration.
type Registration func(map[string]CollectionGenerator, chan *model.PushRequest) CollectionRegistration

func TypeName[T proto.Message]() string {
	ft := new(T)
	return "type.googleapis.com/" + string((*ft).ProtoReflect().Descriptor().FullName())
}

type IntoProto[T proto.Message] interface {
	IntoProto() T
}

type IntoResourceName interface {
	XDSResourceName() string
}

func getKey[T any](t T) string {
	if xx, ok := any(t).(IntoResourceName); ok {
		return xx.XDSResourceName()
	}
	return krt.GetKey(t)
}

// baseCollection is the underlying implementation for both Collection and PerGatewayCollection, which handles
// the common logic of creating a krt.Collection and registering it with the appropriate handlers. The extract
// function is used to determine if the collection is per-gateway and to extract the gateway information from
// the object if needed.
func baseCollection[T IntoProto[TT], TT proto.Message](
	collection krt.Collection[T],
	extract func(o T) types.NamespacedName,
	krtopts krt.OptionsBuilder,
) Registration {
	return func(m map[string]CollectionGenerator, pushChannel chan *model.PushRequest) CollectionRegistration {
		nc := krt.NewCollection(collection, func(ctx krt.HandlerContext, i T) *DiscoveryResource {
			var forGateway *types.NamespacedName
			if extract != nil {
				forGateway = ptr.Of(extract(i))
			}
			return &DiscoveryResource{
				Resource: &discovery.Resource{
					Name:         getKey(i),
					Version:      "",
					Resource:     protoconv.MessageToAny(i.IntoProto()),
					Ttl:          nil,
					CacheControl: nil,
					Metadata:     nil,
				},
				ForGateway: forGateway,
			}
		}, krtopts.WithName(fmt.Sprintf("XDS/%s", TypeName[TT]()))...)

		t := TypeName[TT]()
		m[t] = CollectionGenerator{
			PerGateway: extract != nil,
			Col:        nc,
		}
		start := func(stop <-chan struct{}) {
			if !nc.WaitUntilSynced(stop) {
				return
			}

			nc.RegisterBatch(func(o []krt.Event[DiscoveryResource]) {
				un := make(sets.Set[model.ConfigKey], len(o))
				for _, oo := range o {
					last := oo.Latest()
					// Using namespace to store the type URL, as that's what we filter on for updates
					un.Insert(model.ConfigKey{
						Kind:      kind.Address,
						Name:      last.Name,
						Namespace: t,
					})
				}
				pr := model.PushRequest{
					ConfigsUpdated: un,
				}
				pushChannel <- &pr
			}, true)
		}
		synced := func() bool {
			return nc.HasSynced()
		}
		return CollectionRegistration{
			Start:     start,
			HasSynced: synced,
		}
	}
}

// Collection creates a collection registration for a global collection, where all resources are
// relevant to all gateways.
func Collection[T IntoProto[TT], TT proto.Message](
	collection krt.Collection[T],
	krtopts krt.OptionsBuilder,
) Registration {
	return baseCollection(collection, nil, krtopts)
}

// PerGatewayCollection creates a collection registration for a per-gateway collection, where resources
// can be scoped to a specific gateway. The extract function is used to determine the gateway for each resource.
func PerGatewayCollection[T IntoProto[TT], TT proto.Message](
	collection krt.Collection[T],
	extract func(o T) types.NamespacedName,
	krtopts krt.OptionsBuilder,
) Registration {
	return baseCollection(collection, extract, krtopts)
}

// GenerateDeltas computes discovery resources. This is design to be highly optimized to delta updates,
// and supports *on-demand* client usage. A client can subscribe with a wildcard subscription and get all
// resources (with delta updates), or on-demand and only get responses for specifically subscribed resources.
//
// Incoming requests may be for VIP or Pod IP addresses. However, all responses are Workload resources, which are pod based.
// This means subscribing to a VIP may end up pushing many resources of different name than the request.
// On-demand clients are expected to handle this (for wildcard, this is not applicable, as they don't specify any resources at all).
func (c CollectionGenerator) GenerateDeltas(
	proxy *model.Proxy,
	req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	// Get gw NamespacedName from proxy
	agwName, ok := proxy.Labels[gatewayNameLabel]
	if !ok {
		return nil, nil, model.XdsLogDetails{}, false, fmt.Errorf("proxy missing %s label", gatewayNameLabel)
	}
	gw := types.NamespacedName{
		Namespace: proxy.Metadata.Namespace,
		Name:      agwName,
	}

	if req.IsRequest() {
		// Full update, expect everything
		res := slices.MapFilter(c.Col.List(), func(e DiscoveryResource) **discovery.Resource {
			if !e.IsForGateway(gw) {
				return nil
			}
			return &e.Resource
		})
		toDeleted := w.ResourceNames.Copy()
		for _, r := range res {
			toDeleted.Delete(r.Name)
		}
		deletes := sets.SortedList(toDeleted)
		return res, deletes, model.XdsLogDetails{}, true, nil
	}

	res := make([]*discovery.Resource, 0)
	var deletes []string

	for k := range req.ConfigsUpdated {
		// When configKey kind is Address or AgwResource, the namespace is the type URL.
		if k.Kind != kind.Address && k.Kind != kind.AgwResource && k.Namespace != w.TypeUrl {
			log.Debugf("Skipped config update for type %s. Watched type is %s", k.Namespace, w.TypeUrl)
			continue
		}
		originalKey := k
		var keys []string
		if c.PerGateway {
			// Lookup both unscoped and for our gateway
			keys = []string{types.NamespacedName{}.String() + "/" + k.Name, gw.String() + "/" + k.Name}
		} else {
			// Just lookup the key, no need to worry about gateways
			keys = []string{k.Name}
		}
		found := false
		for _, key := range keys {
			v := c.Col.GetKey(key)
			if v != nil && !v.IsForGateway(gw) {
				v = nil
			}
			if v != nil {
				found = true
				res = append(res, v.Resource)
				break
			}
		}
		if !found {
			deletes = append(deletes, originalKey.Name)
		}
	}

	if len(res) == 0 && len(deletes) == 0 {
		// No changes
		return nil, nil, model.XdsLogDetails{}, false, nil
	}

	return res, deletes, model.XdsLogDetails{}, true, nil
}

// Defined to implement XdsResourceGenerator interface
func (c CollectionGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	resources, _, details, _, err := c.GenerateDeltas(proxy, req, w)
	return resources, details, err
}
