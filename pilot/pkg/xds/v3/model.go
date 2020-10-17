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

package v3

import (
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
)

const (
	ClusterType   = resource.ClusterType
	EndpointType  = resource.EndpointType
	ListenerType  = resource.ListenerType
	RouteType     = resource.RouteType
	SecretType    = resource.SecretType
	NameTableType = "type.googleapis.com/istio.networking.nds.v1.NameTable"
)

// PushOrder defines the order that updates will be pushed in. Any types not listed here will be pushed in random
// order after the types listed here
var PushOrder = []string{ClusterType, EndpointType, ListenerType, RouteType, SecretType}
var KnownPushOrder = map[string]struct{}{
	ClusterType:  {},
	EndpointType: {},
	ListenerType: {},
	RouteType:    {},
	SecretType:   {},
}

// GetShortType returns an abbreviated form of a type, useful for logging or human friendly messages
func GetShortType(typeURL string) string {
	switch typeURL {
	case ClusterType:
		return "CDS"
	case ListenerType:
		return "LDS"
	case RouteType:
		return "RDS"
	case EndpointType:
		return "EDS"
	case SecretType:
		return "SDS"
	case NameTableType:
		return "NDS"
	default:
		return typeURL
	}
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
	case SecretType, EndpointType, RouteType:
		// By XDS spec, these are not wildcard
		return false
	case ClusterType, ListenerType:
		// By XDS spec, these are wildcard
		return true
	default:
		// All of our internal types use wildcard semantics
		return true
	}
}

// GetMetricType returns the form of a type reported for metrics
func GetMetricType(typeURL string) string {
	switch typeURL {
	case ClusterType:
		return "cds"
	case ListenerType:
		return "lds"
	case RouteType:
		return "rds"
	case EndpointType:
		return "eds"
	case SecretType:
		return "sds"
	case NameTableType:
		return "nds"
	default:
		return typeURL
	}
}
