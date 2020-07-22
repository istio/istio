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

package util

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/pkg/config/resource"
)

// ErrorLineForHostInDestination returns the line number of the host in destination
func ErrorLineForHostInDestination(r *resource.Instance, routeRule string, serviceIndex, destinationIndex int) (int, bool) {
	pathKey := fmt.Sprintf("{.spec.%s[%d].route[%d].destination.host}", routeRule, serviceIndex, destinationIndex)
	return FindErrorLine(pathKey, r.Origin.GetFieldMap())
}

// ErrorLineForHostInHTTPMirror returns the line number of the host in destination
func ErrorLineForHostInHTTPMirror(r *resource.Instance, routeRuleIndex int) (int, bool) {
	pathKey := fmt.Sprintf("{.spec.http[%d].mirror.host}", routeRuleIndex)
	return FindErrorLine(pathKey, r.Origin.GetFieldMap())
}

// ErrorLineForVSGateway returns the path of the gateway in VirtualService
func ErrorLineForVSGateway(r *resource.Instance, gatewayIndex int) (int, bool) {
	pathKey := fmt.Sprintf("{.spec.gateways[%d]}", gatewayIndex)
	return FindErrorLine(pathKey, r.Origin.GetFieldMap())
}

// ErrorLineForHTTPRegexURISchemeMethodAuthority returns the path of the regex match in HttpRoute
func ErrorLineForHTTPRegexURISchemeMethodAuthority(r *resource.Instance, httpIndex, matchIndex int, where string) (int, bool) {
	pathKey := fmt.Sprintf("{.spec.http[%d].match[%d].%s.regex}", httpIndex, matchIndex, where)
	return FindErrorLine(pathKey, r.Origin.GetFieldMap())
}

// ErrorLineForHTTPRegexHeaderAndQueryParams returns the path of the regex match in HttpRoute
func ErrorLineForHTTPRegexHeaderAndQueryParams(r *resource.Instance, httpIndex, matchIndex int, where, matchKey string) (int, bool) {
	pathKey := fmt.Sprintf("{.spec.http[%d].match[%d].%s.%s.regex}", httpIndex, matchIndex, where, matchKey)
	return FindErrorLine(pathKey, r.Origin.GetFieldMap())
}

// ErrorLineForHTTPRegexAllowOrigins returns the path of the regex match in HttpRoute
func ErrorLineForHTTPRegexAllowOrigins(r *resource.Instance, httpIndex, allowOriginIndex int) (int, bool) {
	pathKey := fmt.Sprintf("{.spec.http[%d].corsPolicy.allowOrigins[%d].regex}", httpIndex, allowOriginIndex)
	return FindErrorLine(pathKey, r.Origin.GetFieldMap())
}

// ErrorLineForGatewaySelector returns the line number of the gateway selector
func ErrorLineForWorkLoadSelector(r *resource.Instance, workLoadSelector labels.Selector) (int, bool) {
	selectorLabel := FindLabelForSelector(workLoadSelector)
	pathKey := fmt.Sprintf("{.spec.workloadSelector.labels.%s}", selectorLabel)
	return FindErrorLine(pathKey, r.Origin.GetFieldMap())
}

// ErrorLineForFromRegistry returns the line number of fromRegistry in the mesh networks
func ErrorLineForPortInService(r *resource.Instance, portIndex int) (int, bool) {
	keyPath := fmt.Sprintf(".spec.ports[%d].port}", portIndex)
	return FindErrorLine(keyPath, r.Origin.GetFieldMap())
}

// ErrorLineForFromRegistry returns the line number of fromRegistry in the mesh networks
func ErrorLineForFromRegistry(r *resource.Instance, networkName string, endPointIndex int) (int, bool) {
	keyPath := fmt.Sprintf("{.networks.%s.endpoints[%d]}", networkName, endPointIndex)
	return FindErrorLine(keyPath, r.Origin.GetFieldMap())
}

// ErrorLineForContainerImage returns the line number of the image in the container
func ErrorLineForContainerImage(r *resource.Instance, containerIndex int) (int, bool) {
	keyPath := fmt.Sprintf("{.spec.containers[%d].image}", containerIndex)
	return FindErrorLine(keyPath, r.Origin.GetFieldMap())
}

// ErrorLineForMetaDataNameSpace returns the line number of the metadata.namespace
func ErrorLineForMetaDataNameSpace(r *resource.Instance) (int, bool) {
	keyPath := "{.metadata.namespace}"
	return FindErrorLine(keyPath, r.Origin.GetFieldMap())
}

// ErrorLineForMetaDataName returns the line number of the metadata.name
func ErrorLineForMetaDataName(r *resource.Instance) (int, bool) {
	keyPath := "{.metadata.name}"
	return FindErrorLine(keyPath, r.Origin.GetFieldMap())
}

// ErrorLineForAuthorizationPolicyNameSpace returns the line number of the namespaces for authorizationPolicy
func ErrorLineForAuthorizationPolicyNameSpace(r *resource.Instance, ruleIndex, fromIndex, namespaceIndex int) (int, bool) {
	pathKey := fmt.Sprintf("{.spec.rules[%d].from[%d].source.namespaces[%d]}", ruleIndex, fromIndex, namespaceIndex)
	return FindErrorLine(pathKey, r.Origin.GetFieldMap())
}

// FinedErrorLineNum returns the error line number with the annotation name and the field map from resource.Origin as the input
func ErrorLineForAnnotation(r *resource.Instance, annotationName string) (int, bool) {
	path := fmt.Sprintf("{.metadata.annotations.%s}", annotationName)
	return FindErrorLine(path, r.Origin.GetFieldMap())
}

// ErrorLineForGatewaySelector returns the path of the gateway selector
func ErrorLineForGatewaySelector(r *resource.Instance, gwSelector labels.Selector) (int, bool) {
	selectorLabel := FindLabelForSelector(gwSelector)
	pathKey := fmt.Sprintf("{.spec.selector.%s}", selectorLabel)
	return FindErrorLine(pathKey, r.Origin.GetFieldMap())
}

// ErrorLineForCredentialName returns the path of the gateway selector
func ErrorLineForCredentialName(r *resource.Instance, serverIndex int) (int, bool) {
	pathKey := fmt.Sprintf("{.spec.servers[%d].tls.credentialName}", serverIndex)
	return FindErrorLine(pathKey, r.Origin.GetFieldMap())
}

// FindErrorLine returns the line number of the input key from the input map, and true if retrieving successfully,
// else return -1 and false
func FindErrorLine(key string, m map[string]int) (int, bool) {
	var line int

	// Check if the map exists
	if m == nil {
		return -1, false
	}

	// Check if the path key exists in the map
	if v, ok := m[key]; ok {
		line = v
	} else {
		return -1, false
	}
	return line, true
}

// FindLabelForSelector returns the label for the k8s labels.Selector
func FindLabelForSelector(selector labels.Selector) string {
	s := selector.String()
	equalIndex := strings.Index(s, "=")
	return s[:equalIndex]
}
