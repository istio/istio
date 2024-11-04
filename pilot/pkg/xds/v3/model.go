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
	"istio.io/istio/pkg/model"
)

const (
	ClusterType                = model.ClusterType
	EndpointType               = model.EndpointType
	ListenerType               = model.ListenerType
	RouteType                  = model.RouteType
	SecretType                 = model.SecretType
	ExtensionConfigurationType = model.ExtensionConfigurationType
	NameTableType              = model.NameTableType
	HealthInfoType             = model.HealthInfoType
	ProxyConfigType            = model.ProxyConfigType
	DebugType                  = model.DebugType
	BootstrapType              = model.BootstrapType
	AddressType                = model.AddressType
	WorkloadType               = model.WorkloadType
	WorkloadAuthorizationType  = model.WorkloadAuthorizationType

	// nolint
	HttpProtocolOptionsType = "envoy.extensions.upstreams.http.v3.HttpProtocolOptions"
)

// GetShortType returns an abbreviated form of a type, useful for logging or human friendly messages
func GetShortType(typeURL string) string {
	return model.GetShortType(typeURL)
}

// GetMetricType returns the form of a type reported for metrics
func GetMetricType(typeURL string) string {
	return model.GetMetricType(typeURL)
}

// GetResourceType returns resource form of an abbreviated form
func GetResourceType(shortType string) string {
	return model.GetResourceType(shortType)
}
