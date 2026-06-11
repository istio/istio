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

// Package gatewayapi exposes Gateway API CRD version requirements shared by the
// CRD watcher (which filters CRDs below the minimum) and the istioctl analyzer
// (which surfaces a clear finding when the cluster's CRDs are too old).
package gatewayapi

import (
	"github.com/Masterminds/semver/v3"
)

// MinimumCRDVersions is the minimum Gateway API bundle version (the value of the
// `gateway.networking.k8s.io/bundle-version` annotation on the CRD) required by
// this Istio binary, keyed by CRD name. CRDs below the listed version are not
// processed by istiod and will not be watched.
var MinimumCRDVersions = map[string]*semver.Version{
	"grpcroutes.gateway.networking.k8s.io":         semver.New(1, 1, 0, "", ""),
	"backendtlspolicies.gateway.networking.k8s.io": semver.New(1, 4, 0, "", ""),
	"tlsroutes.gateway.networking.k8s.io":          semver.New(1, 5, 0, "", ""),
}
