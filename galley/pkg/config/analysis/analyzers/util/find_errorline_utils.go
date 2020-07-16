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
	"istio.io/istio/pkg/config/resource"
	"k8s.io/apimachinery/pkg/labels"
	"strings"
)

// PathInfo stores the index information to indicate the path about a httpMatch
type PathInfo struct {
	HttpIndex        int
	MatchIndex       int
	AllowOriginIndex int
	HeaderKey        string
	QueryParamsKey   string
}

// ErrorLineForHostInDestination returns the line number of the host in destination
func ErrorLineForHostInDestination(routeRule string, serviceIndex int, destinationIndex int, r *resource.Instance) int {
	pathKey := "{.spec." + routeRule + "[" +
		fmt.Sprintf("%d", serviceIndex) + "].route[" +
		fmt.Sprintf("%d", destinationIndex) + "].destination.host}"
	line := FindErrorLine(pathKey, r.Origin.GetFieldMap())
	return line
}

// ErrorLineForHostInHttpMirror returns the line number of the host in destination
func ErrorLineForHostInHttpMirror(routeRuleIndex int, r *resource.Instance) int {
	pathKey := "{.spec.http[" + fmt.Sprintf("%d", routeRuleIndex) + "].mirror.host}"
	line := FindErrorLine(pathKey, r.Origin.GetFieldMap())
	return line
}

// ErrorLineForVSGateway returns the path of the gateway in VirtualService
func ErrorLineForVSGateway(gatewayIndex int, r *resource.Instance) int {
	pathKey := "{.spec.gateways[" + fmt.Sprintf("%d", gatewayIndex) + "]}"
	line := FindErrorLine(pathKey, r.Origin.GetFieldMap())
	return line
}

// ErrorLineForHttpRegex returns the path of the regex match in HttpRoute
func ErrorLineForHttpRegex(pathInfo PathInfo, where string, r *resource.Instance) int {
	pathKey := "{.spec.http[" + fmt.Sprintf("%d", pathInfo.HttpIndex) + "]"

	switch where {
	case "corsPolicy.allowOrigins":
		pathKey += ".corsPolicy.allowOrigins[" + fmt.Sprintf("%d", pathInfo.AllowOriginIndex) + "].regex}"
	case "headers":
		pathKey += ".match[" + fmt.Sprintf("%d", pathInfo.MatchIndex) + "].headers." + pathInfo.HeaderKey + ".regex}"
	case "queryParams":
		pathKey += ".match[" + fmt.Sprintf("%d", pathInfo.MatchIndex) + "].queryParams." + pathInfo.QueryParamsKey + ".regex}"
	default:
		pathKey += ".match[" + fmt.Sprintf("%d", pathInfo.MatchIndex) + "]." + where + ".regex}"
	}

	line := FindErrorLine(pathKey, r.Origin.GetFieldMap())
	return line
}

// ErrorLineForGatewaySelector returns the line number of the gateway selector
func ErrorLineForWorkLoadSelector(workLoadSelector labels.Selector, r *resource.Instance) int {
	pathKey := "{.spec.workloadSelector.labels"
	selectorLabel := FindLabelForSelector(workLoadSelector)
	pathKey += "." + selectorLabel + "}"
	line := FindErrorLine(pathKey, r.Origin.GetFieldMap())
	return line
}

// ErrorLineForFromRegistry returns the line number of fromRegistry in the mesh networks
func ErrorLineForPortInService(portIndex int, r *resource.Instance) int {
	keyPath := "{.spec.ports[" + fmt.Sprintf("%d", portIndex) + "].port" +"}"
	line := FindErrorLine(keyPath, r.Origin.GetFieldMap())
	return line
}

// ErrorLineForFromRegistry returns the line number of fromRegistry in the mesh networks
func ErrorLineForFromRegistry(networkName string, endPointIndex int, r *resource.Instance) int {
	keyPath := "{.networks." + networkName + ".endpoints[" + fmt.Sprintf("%d", endPointIndex) + "]}"
	line := FindErrorLine(keyPath, r.Origin.GetFieldMap())
	return line
}

// ErrorLineForContainerImage returns the line number of the image in the container
func ErrorLineForContainerImage(containerIndex int, r *resource.Instance) int {
	keyPath := "{.spec.spec.containers[}" + fmt.Sprintf("%d", containerIndex) + "].image}"
	line := FindErrorLine(keyPath, r.Origin.GetFieldMap())
	return line
}

// ErrorLineForMetaDataNameSpace returns the line number of the metadata.namespace
func ErrorLineForMetaDataNameSpace(r *resource.Instance) int {
	keyPath := "{.metadata.namespace}"
	line := FindErrorLine(keyPath, r.Origin.GetFieldMap())
	return line
}

// ErrorLineForMetaDataName returns the line number of the metadata.name
func ErrorLineForMetaDataName(r *resource.Instance) int {
	keyPath := "{.metadata.name}"
	line := FindErrorLine(keyPath, r.Origin.GetFieldMap())
	return line
}

// ErrorLineForAuthorizationPolicyNameSpace returns the line number of the namespaces for authorizationPolicy
func ErrorLineForAuthorizationPolicyNameSpace(ruleIndex int, fromIndex int, namespaceIndex int, r *resource.Instance) int {
	pathKey := "{.spec.rules[" + fmt.Sprintf("%d", ruleIndex) + "].from[" + fmt.Sprintf("%d", fromIndex) +
		"].source.namespaces[" + fmt.Sprintf("%d", namespaceIndex) + "]}"
	line := FindErrorLine(pathKey, r.Origin.GetFieldMap())
	return line
}

// FinedErrorLineNum returns the error line number with the annotation name and the field map from resource.Origin as the input
func ErrorLineForAnnotation(annotationName string, r *resource.Instance) int {
	path := "{.metadata.annotations"
	path += "." + annotationName + "}"
	line := FindErrorLine(path, r.Origin.GetFieldMap())
	return line
}

// ErrorLineForGatewaySelector returns the path of the gateway selector
func ErrorLineForGatewaySelector(gwSelector labels.Selector, r *resource.Instance) int {
	pathKey := "{.spec.selector"
	selectorLabel := FindLabelForSelector(gwSelector)
	pathKey += "." + selectorLabel + "}"
	line := FindErrorLine(pathKey, r.Origin.GetFieldMap())
	return line
}

// ErrorLineForCredentialName returns the path of the gateway selector
func ErrorLineForCredentialName(serverIndex int, r *resource.Instance) int {
	pathKey := "{.spec.servers[" + fmt.Sprintf("%d", serverIndex) + "].tls.credentialName}"
	line := FindErrorLine(pathKey, r.Origin.GetFieldMap())
	return line
}

// FindErrorLine returns the line number of the input key from the input map
func FindErrorLine(key string, m map[string]int) int {
	var line int

	// Check if the map exists
	if m == nil { return line }

	if v, ok := m[key]; ok {
		line = v
	}
	return line
}

// FindLabelForSelector returns the label for the k8s labels.Selector
func FindLabelForSelector(selector labels.Selector) string {
	s := selector.String()
	equalIndex := strings.Index(s, "=")
	return s[:equalIndex]
}
