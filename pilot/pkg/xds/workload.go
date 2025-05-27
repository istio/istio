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

package xds

import (
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

type WorkloadGenerator struct {
	Server *DiscoveryServer
}

var (
	_ model.XdsResourceGenerator      = &WorkloadGenerator{}
	_ model.XdsDeltaResourceGenerator = &WorkloadGenerator{}
)

// GenerateDeltas computes Workload resources. This is design to be highly optimized to delta updates,
// and supports *on-demand* client usage. A client can subscribe with a wildcard subscription and get all
// resources (with delta updates), or on-demand and only get responses for specifically subscribed resources.
//
// Incoming requests may be for VIP or Pod IP addresses. However, all responses are Workload resources, which are pod based.
// This means subscribing to a VIP may end up pushing many resources of different name than the request.
// On-demand clients are expected to handle this (for wildcard, this is not applicable, as they don't specify any resources at all).
func (e WorkloadGenerator) GenerateDeltas(
	proxy *model.Proxy,
	req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	addresses := req.AddressesUpdated
	isReq := req.IsRequest()
	if !isReq && len(addresses) == 0 {
		// Nothing changed...
		return nil, nil, model.XdsLogDetails{}, false, nil
	}

	if !w.Wildcard {
		return e.generateDeltasOndemand(proxy, req, w)
	}

	reqAddresses := addresses
	if isReq {
		reqAddresses = nil
	}
	addrs, removed := e.Server.Env.AddressInformation(reqAddresses)
	// Note: while "removed" is a weird name for a resource that never existed, this is how the spec works:
	// https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol#id2
	have := sets.New[string]()
	resources := make(model.Resources, 0, len(addrs))
	for _, addr := range addrs {
		resources = appendAddress(addr, w.TypeUrl, nil, have, resources)
	}

	if isReq {
		// If it's a full push, AddressInformation won't have info to compute the full set of removals.
		// Instead, we need can see what resources are missing that we were subscribe to; those were removed.
		// During a request, a client will send a subscription request with its current state, so our removals will be:
		// ZtunnelCurrentResources - IstiodCurrentResources (+ empty `removed`).
		removed = req.Delta.Subscribed.Difference(have).Merge(removed)
	}

	// Due to the high resource count in WDS at scale, we elide updating ResourceNames here.
	// A name will be ~50-100 bytes. So 50k pods would be 5MB per XDS client (plus overheads around GC, sets, fragmentation, etc).
	// Fortunately, we do not actually need this: the only time we need to know the state in Ztunnel is on reconnection which we handle from
	// `req.Delta.Subscribed`.

	return resources, removed.UnsortedList(), model.XdsLogDetails{}, true, nil
}

func appendAddress(addr model.AddressInfo, requestedType string, aliases []string, have sets.Set[string], resources model.Resources) model.Resources {
	n := addr.ResourceName()
	have.Insert(n)
	switch requestedType {
	case v3.WorkloadType:
		if addr.GetWorkload() != nil {
			resources = append(resources, &discovery.Resource{
				Name:     n,
				Aliases:  aliases,
				Resource: protoconv.MessageToAny(addr.GetWorkload()), // TODO: pre-marshal
			})
		}
	case v3.AddressType:
		proto := addr.Marshaled
		if proto == nil {
			proto = protoconv.MessageToAny(addr)
		}

		resources = append(resources, &discovery.Resource{
			Name:     n,
			Aliases:  aliases,
			Resource: proto,
		})
	}
	return resources
}

func (e WorkloadGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	resources, _, details, _, err := e.GenerateDeltas(proxy, req, w)
	return resources, details, err
}

func (e WorkloadGenerator) generateDeltasOndemand(
	proxy *model.Proxy,
	req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	isReq := req.IsRequest()
	subs := w.ResourceNames

	var addresses sets.String
	if isReq {
		// this is from request, we only send response for the subscribed address
		// At t0, a client request A, we only send A and additional resources back to the client.
		// At t1, a client request B, we only send B and additional resources back to the client, no A here.
		addresses = req.Delta.Subscribed
	} else {
		// this is from the external triggers instead of request
		// send response for all the subscribed intersect with the updated
		addresses = req.AddressesUpdated.Intersection(subs)
	}

	// We only need this for on-demand. This allows us to subscribe the client to resources they
	// didn't explicitly request.
	// For wildcard, they subscribe to everything already.
	additional := e.Server.Env.AdditionalPodSubscriptions(proxy, addresses, subs)
	if addresses == nil {
		addresses = additional
	} else {
		addresses.Merge(additional)
	}

	if len(addresses) == 0 {
		if isReq {
			// We need to respond for requests, even if we have nothing to respond with
			return make(model.Resources, 0), nil, model.XdsLogDetails{}, false, nil
		}
		// For NOP pushes, no need
		return nil, nil, model.XdsLogDetails{}, false, nil
	}
	addrs, removed := e.Server.Env.AddressInformation(addresses)
	// Note: while "removed" is a weird name for a resource that never existed, this is how the spec works:
	// https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol#id2
	have := sets.New[string]()
	resources := make(model.Resources, 0, len(addrs))
	for _, addr := range addrs {
		aliases := addr.Aliases()
		removed.DeleteAll(aliases...)
		resources = appendAddress(addr, w.TypeUrl, aliases, have, resources)
	}

	proxy.Lock()
	defer proxy.Unlock()
	// For on-demand, we may have requested a VIP but gotten Pod IPs back. We need to update
	// the internal book-keeping to subscribe to the Pods, so that we push updates to those Pods.
	w.ResourceNames = subs.Merge(have)
	return resources, removed.UnsortedList(), model.XdsLogDetails{}, true, nil
}

type WorkloadRBACGenerator struct {
	Server *DiscoveryServer
}

func (e WorkloadRBACGenerator) GenerateDeltas(
	proxy *model.Proxy,
	req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	var updatedPolicies sets.Set[model.ConfigKey]
	expected := sets.New[string]()
	if req.Forced {
		// Full update, expect everything
		expected.Merge(w.ResourceNames)
	} else {
		// The ambient store will send all of these as kind.AuthorizationPolicy, even if generated from PeerAuthentication,
		// so we can only fetch these ones.
		updatedPolicies = model.ConfigsOfKind(req.ConfigsUpdated, kind.AuthorizationPolicy)

		if len(updatedPolicies) == 0 {
			// This was a incremental push for a resource we don't watch... skip
			return nil, nil, model.DefaultXdsLogDetails, false, nil
		}

		for k := range updatedPolicies {
			expected.Insert(k.Namespace + "/" + k.Name)
		}
	}

	resources := make(model.Resources, 0)
	policies := e.Server.Env.Policies(updatedPolicies)
	for _, p := range policies {
		n := p.ResourceName()
		expected.Delete(n) // delete the generated policy name, left the removed ones
		resources = append(resources, &discovery.Resource{
			Name:     n,
			Resource: protoconv.MessageToAny(p.Authorization),
		})
	}

	return resources, sets.SortedList(expected), model.XdsLogDetails{}, true, nil
}

func (e WorkloadRBACGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	resources, _, details, _, err := e.GenerateDeltas(proxy, req, w)
	return resources, details, err
}

var (
	_ model.XdsResourceGenerator      = &WorkloadRBACGenerator{}
	_ model.XdsDeltaResourceGenerator = &WorkloadRBACGenerator{}
)
