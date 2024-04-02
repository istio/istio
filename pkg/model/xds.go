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

package model

import (
	"strings"
)

const (
	APITypePrefix   = "type.googleapis.com/"
	envoyTypePrefix = APITypePrefix + "envoy."

	ClusterType                = APITypePrefix + "envoy.config.cluster.v3.Cluster"
	EndpointType               = APITypePrefix + "envoy.config.endpoint.v3.ClusterLoadAssignment"
	ListenerType               = APITypePrefix + "envoy.config.listener.v3.Listener"
	RouteType                  = APITypePrefix + "envoy.config.route.v3.RouteConfiguration"
	SecretType                 = APITypePrefix + "envoy.extensions.transport_sockets.tls.v3.Secret"
	ExtensionConfigurationType = APITypePrefix + "envoy.config.core.v3.TypedExtensionConfig"

	NameTableType   = APITypePrefix + "istio.networking.nds.v1.NameTable"
	HealthInfoType  = APITypePrefix + "istio.v1.HealthInformation"
	ProxyConfigType = APITypePrefix + "istio.mesh.v1alpha1.ProxyConfig"
	// DebugType requests debug info from istio, a secured implementation for istio debug interface.
	DebugType                 = "istio.io/debug"
	BootstrapType             = APITypePrefix + "envoy.config.bootstrap.v3.Bootstrap"
	AddressType               = APITypePrefix + "istio.workload.Address"
	WorkloadType              = APITypePrefix + "istio.workload.Workload"
	WorkloadAuthorizationType = APITypePrefix + "istio.security.Authorization"
)

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
	case ProxyConfigType:
		return "PCDS"
	case ExtensionConfigurationType:
		return "ECDS"
	case AddressType, WorkloadType:
		return "WDS"
	case WorkloadAuthorizationType:
		return "WADS"
	default:
		return typeURL
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
	case ProxyConfigType:
		return "pcds"
	case ExtensionConfigurationType:
		return "ecds"
	case BootstrapType:
		return "bds"
	case AddressType, WorkloadType:
		return "wds"
	case WorkloadAuthorizationType:
		return "wads"
	default:
		return typeURL
	}
}

// GetResourceType returns resource form of an abbreviated form
func GetResourceType(shortType string) string {
	s := strings.ToUpper(shortType)
	switch s {
	case "CDS":
		return ClusterType
	case "LDS":
		return ListenerType
	case "RDS":
		return RouteType
	case "EDS":
		return EndpointType
	case "SDS":
		return SecretType
	case "NDS":
		return NameTableType
	case "PCDS":
		return ProxyConfigType
	case "ECDS":
		return ExtensionConfigurationType
	case "WDS":
		return AddressType
	case "WADS":
		return WorkloadAuthorizationType
	default:
		return shortType
	}
}

// IsEnvoyType checks whether the typeURL is a valid Envoy type.
func IsEnvoyType(typeURL string) bool {
	return strings.HasPrefix(typeURL, envoyTypePrefix)
}
