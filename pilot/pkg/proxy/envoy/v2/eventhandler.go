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

package v2

import v3 "istio.io/istio/pilot/pkg/proxy/envoy/v3"

// DistributionType represents the type of object we are tracking. This is distinct from Envoy's TypeUrl
// as TypeUrl is versioned, whereas DistributionType is not
type DistributionType string

const (
	ClusterDistributionType  DistributionType = "Cluster"
	ListenerDistributionType DistributionType = "Listener"
	RouteDistributionType    DistributionType = "Route"
	EndpointDistributionType DistributionType = "Endpoint"
	UnknownDistributionType  DistributionType = ""
)

var AllDistributionTypes = []DistributionType{
	ClusterDistributionType,
	ListenerDistributionType,
	RouteDistributionType,
	EndpointDistributionType,
}

func TypeURLToDistributionType(typeURL string) DistributionType {
	switch typeURL {
	case ClusterType, v3.ClusterType:
		return ClusterDistributionType
	case EndpointType, v3.EndpointType:
		return EndpointDistributionType
	case RouteType, v3.RouteType:
		return RouteDistributionType
	case ListenerType, v3.ListenerType:
		return ListenerDistributionType
	default:
		return UnknownDistributionType
	}
}

// EventHandler allows for generic monitoring of xDS ACKS and disconnects, for the purpose of tracking
// Config distribution through the mesh.
type DistributionStatusCache interface {
	// RegisterEvent notifies the implementer of an xDS ACK, and must be non-blocking
	RegisterEvent(conID string, distributionType DistributionType, nonce string)
	RegisterDisconnect(s string, types []DistributionType)
	QueryLastNonce(conID string, distributionType DistributionType) (noncePrefix string)
}
