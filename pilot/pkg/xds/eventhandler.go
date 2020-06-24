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
	v2 "istio.io/istio/pilot/pkg/xds/v2"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

// EventType represents the type of object we are tracking. This is distinct from Envoy's TypeUrl
// as TypeUrl is versioned, whereas EventType is not
type EventType string

const (
	ClusterEventType  EventType = "Cluster"
	ListenerEventType EventType = "Listener"
	RouteEventType    EventType = "Route"
	EndpointEventType EventType = "Endpoint"
	UnknownEventType  EventType = ""
)

var AllEventTypes = []EventType{
	ClusterEventType,
	ListenerEventType,
	RouteEventType,
	EndpointEventType,
}

func TypeURLToEventType(typeURL string) EventType {
	switch typeURL {
	case v2.ClusterType, v3.ClusterType:
		return ClusterEventType
	case v2.EndpointType, v3.EndpointType:
		return EndpointEventType
	case v2.RouteType, v3.RouteType:
		return RouteEventType
	case v2.ListenerType, v3.ListenerType:
		return ListenerEventType
	default:
		return UnknownEventType
	}
}

// EventHandler allows for generic monitoring of xDS ACKS and disconnects, for the purpose of tracking
// Config distribution through the mesh.
type DistributionStatusCache interface {
	// RegisterEvent notifies the implementer of an xDS ACK, and must be non-blocking
	RegisterEvent(conID string, eventType EventType, nonce string)
	RegisterDisconnect(s string, types []EventType)
	QueryLastNonce(conID string, eventType EventType) (noncePrefix string)
}
