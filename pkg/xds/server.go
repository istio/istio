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

	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/util/sets"
)

// ResourceDelta records the difference in requested resources by an XDS client
type ResourceDelta struct {
	// Subscribed indicates the client requested these additional resources
	Subscribed sets.String
	// Unsubscribed indicates the client no longer requires these resources
	Unsubscribed sets.String
}

func (rd ResourceDelta) IsEmpty() bool {
	return len(rd.Subscribed) == 0 && len(rd.Unsubscribed) == 0
}

type Resources = []*discovery.Resource

// WatchedResource tracks an active DiscoveryRequest subscription.
type WatchedResource struct {
	// TypeUrl is copied from the DiscoveryRequest.TypeUrl that initiated watching this resource.
	// nolint
	TypeUrl string

	// ResourceNames tracks the list of resources that are actively watched.
	// For LDS and CDS, all resources of the TypeUrl type are watched if it is empty.
	// For endpoints the resource names will have list of clusters and for clusters it is empty.
	// For Delta Xds, all resources of the TypeUrl that a client has subscribed to.
	ResourceNames []string

	// Wildcard indicates the subscription is a wildcard subscription. This only applies to types that
	// allow both wildcard and non-wildcard subscriptions.
	Wildcard bool

	// NonceSent is the nonce sent in the last sent response. If it is equal with NonceAcked, the
	// last message has been processed. If empty: we never sent a message of this type.
	NonceSent string

	// NonceAcked is the last acked message.
	NonceAcked string

	// AlwaysRespond, if true, will ensure that even when a request would otherwise be treated as an
	// ACK, it will be responded to. This typically happens when a proxy reconnects to another instance of
	// Istiod. In that case, Envoy expects us to respond to EDS/RDS/SDS requests to finish warming of
	// clusters/listeners.
	// Typically, this should be set to 'false' after response; keeping it true would likely result in an endless loop.
	AlwaysRespond bool

	// LastResources tracks the contents of the last push.
	// This field is extremely expensive to maintain and is typically disabled
	LastResources Resources
}

type Watcher interface {
	DeleteWatchedResource(url string)
	GetWatchedResource(url string) *WatchedResource
	NewWatchedResource(url string, names []string)
	UpdateWatchedResource(string, func(*WatchedResource) *WatchedResource)
	GetID() string
}

// IsWildcardTypeURL checks whether a given type is a wildcard type
// https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol#how-the-client-specifies-what-resources-to-return
// If the list of resource names becomes empty, that means that the client is no
// longer interested in any resources of the specified type. For Listener and
// Cluster resource types, there is also a “wildcard” mode, which is triggered
// when the initial request on the stream for that resource type contains no
// resource names.
func IsWildcardTypeURL(typeURL string) bool {
	switch typeURL {
	case model.SecretType, model.EndpointType, model.RouteType, model.ExtensionConfigurationType:
		// By XDS spec, these are not wildcard
		return false
	case model.ClusterType, model.ListenerType:
		// By XDS spec, these are wildcard
		return true
	default:
		// All of our internal types use wildcard semantics
		return true
	}
}
