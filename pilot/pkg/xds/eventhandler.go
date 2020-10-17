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

import v3 "istio.io/istio/pilot/pkg/xds/v3"

// EventType represents the type of object we are tracking, mapping to envoy TypeUrl.
type EventType = string

var AllEventTypes = map[EventType]struct{}{
	v3.ClusterType:  {},
	v3.ListenerType: {},
	v3.RouteType:    {},
	v3.EndpointType: {},
}

// AllEventTypesList is AllEventTypes in list form, for convenience
var AllEventTypesList = []EventType{v3.ClusterType, v3.ListenerType, v3.RouteType, v3.EndpointType}

// EventHandler allows for generic monitoring of xDS ACKS and disconnects, for the purpose of tracking
// Config distribution through the mesh.
type DistributionStatusCache interface {
	// RegisterEvent notifies the implementer of an xDS ACK, and must be non-blocking
	RegisterEvent(conID string, eventType EventType, nonce string)
	RegisterDisconnect(s string, types []EventType)
	QueryLastNonce(conID string, eventType EventType) (noncePrefix string)
}
