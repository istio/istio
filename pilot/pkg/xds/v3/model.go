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
	"strings"

	resource "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
)

const (
	envoyTypePrefix = resource.APITypePrefix + "envoy."

	ClusterType                = resource.ClusterType
	EndpointType               = resource.EndpointType
	ListenerType               = resource.ListenerType
	RouteType                  = resource.RouteType
	SecretType                 = resource.SecretType
	ExtensionConfigurationType = resource.ExtensionConfigType

	NameTableType   = resource.APITypePrefix + "istio.networking.nds.v1.NameTable"
	HealthInfoType  = resource.APITypePrefix + "istio.v1.HealthInformation"
	ProxyConfigType = resource.APITypePrefix + "istio.mesh.v1alpha1.ProxyConfig"
	// DebugType requests debug info from istio, a secured implementation for istio debug interface.
	DebugType     = "istio.io/debug"
	BootstrapType = resource.APITypePrefix + "envoy.config.bootstrap.v3.Bootstrap"

	// nolint
	HttpProtocolOptionsType = "envoy.extensions.upstreams.http.v3.HttpProtocolOptions"
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
	default:
		return typeURL
	}
}

// IsEnvoyType checks whether the typeURL is a valid Envoy type.
func IsEnvoyType(typeURL string) bool {
	return strings.HasPrefix(typeURL, envoyTypePrefix)
}
