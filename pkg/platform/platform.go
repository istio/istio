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

package platform

import "istio.io/istio/pkg/env"

const (
	Default   = ""
	OpenShift = "openshift"
	GCP       = "gcp"
)

var Platform = env.Register(
	"PLATFORM",
	Default,
	"Platform where Istio is deployed. Possible values are \"openshift\" and \"gcp\"",
)

// IsDefault returns true if the platform is the Default one
func IsDefault() bool {
	return Platform.Get() == Default
}

// IsOpenShift returns true if the platform is OpenShift
func IsOpenShift() bool {
	return Platform.Get() == OpenShift
}

// IsGCP returns true if the platform is GCP
func IsGCP() bool {
	return Platform.Get() == GCP
}
