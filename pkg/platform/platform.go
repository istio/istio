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

type PlatformType string

const (
	K8s       PlatformType = "k8s"
	OpenShift PlatformType = "openshift"
	GCP       PlatformType = "gcp"
)

var Platform = env.Register(
	"PLATFORM",
	K8s,
	"Platform where Istio is deployed. Possible values are: k8s (default), openshift and gcp",
).Get()

// IsK8s returns true if the platform is K8s
func IsK8s() bool {
	return Platform == K8s
}

// IsOpenShift returns true if the platform is OpenShift
func IsOpenShift() bool {
	return Platform == OpenShift
}

// IsGCP returns true if the platform is GCP
func IsGCP() bool {
	return Platform == GCP
}
