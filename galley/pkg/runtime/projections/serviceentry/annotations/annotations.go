// Copyright 2019 Istio Authors
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

package annotations

const (
	// ServiceVersion provides the raw resource version from the most recent k8s Service update. This will always
	// be available for synthetic service entries.
	ServiceVersion = "networking.istio.io/serviceVersion"

	// EndpointsVersion provides the raw resource version of the most recent k8s Endpoints update (if available).
	EndpointsVersion = "networking.istio.io/endpointsVersion"

	// NotReadyEndpoints is an annotation providing the "NotReadyAddresses" from the Kubernetes Endpoints
	// resource. The value is a comma-separated list of IP:port.
	NotReadyEndpoints = "networking.istio.io/notReadyEndpoints"
)
