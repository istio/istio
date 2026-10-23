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

package gateway

import (
	"sigs.k8s.io/gateway-api/pkg/features"
)

// TODO: Fix skipped tests and reenable these features.
var skippedExtendedFeatures = []features.Feature{
	// GatewayBackendClientCertificateFeature, GatewayInvalidTLSBackendConfiguration
	features.GatewayBackendClientCertificateFeature,
	// HTTPRouteHTTPSListenerDetectMisdirectedRequests
	features.GatewayHTTPSListenerDetectMisdirectedRequestsFeature,
	// UDPRoute* - Istio does not support UDPRoute
	features.UDPRouteFeature,
	// The following features were added in v1.7.0
	// GatewayRouteHostnameIntersectionPrecedence: route precedence by original route hostname specificity
	features.GatewayRouteHostnameIntersectionPrecedenceFeature,
	// HTTPRouteBackendURLRewrite: URLRewrite filter on backendRefs
	features.HTTPRouteBackendURLRewriteFeature,
}

var SupportedFeatures = features.AllFeatures.Clone().Delete(skippedExtendedFeatures...)
