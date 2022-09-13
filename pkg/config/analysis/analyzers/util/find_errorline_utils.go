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

	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/resource"
)

const (
	// Path templates for different fields with different paths, may edited by future developers if not covered in this list
	// Use the path template to find the exact line number for the field

	// Path for host in VirtualService.
	// Required parameters: route rule, route rule index, route index.
	DestinationHost = "{.spec.%s[%d].route[%d].destination.host}"

	// Path for mirror host in VirtualService.
	// Required parameters: http index.
	MirrorHost = "{.spec.http[%d].mirror.host}"

	// Path for VirtualService gateway.
	// Required parameters: gateway index.
	VSGateway = "{.spec.gateways[%d]}"

	// Path for regex match of uri, scheme, method and authority.
	// Required parameters: http index, match index, where to match.
	URISchemeMethodAuthorityRegexMatch = "{.spec.http[%d].match[%d].%s.regex}"

	// Path for regex match of headers and queryParams.
	// Required parameters: http index, match index, where to match, match key.
	HeaderAndQueryParamsRegexMatch = "{.spec.http[%d].match[%d].%s.%s.regex}"

	// Path for regex match of allowOrigins.
	// Required parameters: http index, allowOrigins index.
	AllowOriginsRegexMatch = "{.spec.http[%d].corsPolicy.allowOrigins[%d].regex}"

	// Path for workload selector.
	// Required parameters: selector label.
	WorkloadSelector = "{.spec.workloadSelector.labels.%s}"

	// Path for port from ports collections.
	// Required parameters: port index.
	PortInPorts = "{.spec.ports[%d].port}"

	// Path for fromRegistry in the mesh networks.
	// Required parameters: network name, endPoint index.
	FromRegistry = "{.networks.%s.endpoints[%d]}"

	// Path for the image in the container.
	// Required parameters: container index.
	ImageInContainer = "{.spec.containers[%d].image}"

	// Path for namespace in metadata.
	// Required parameters: none.
	MetadataNamespace = "{.metadata.namespace}"

	// Path for name in metadata.
	// Required parameters: none.
	MetadataName = "{.metadata.name}"

	// Path for namespace in authorizationPolicy.
	// Required parameters: rule index, from index, namespace index.
	AuthorizationPolicyNameSpace = "{.spec.rules[%d].from[%d].source.namespaces[%d]}"

	// Path for annotation.
	// Required parameters: annotation name.
	Annotation = "{.metadata.annotations.%s}"

	// Path for selector in Gateway.
	// Required parameters: selector label.
	GatewaySelector = "{.spec.selector.%s}"

	// Path for credentialName.
	// Required parameters: server index.
	CredentialName = "{.spec.servers[%d].tls.credentialName}"

	// Path for Port in ServiceEntry.
	// Required parameters: port index.
	ServiceEntryPort = "{.spec.ports[%d].name}"

	// Path for DestinationRule tls certificate.
	// Required parameters: none.
	DestinationRuleTLSCert = "{.spec.trafficPolicy.tls.caCertificates}"

	// Path for DestinationRule port-level tls certificate.
	// Required parameters: portLevelSettings index.
	DestinationRuleTLSPortLevelCert = "{.spec.trafficPolicy.portLevelSettings[%d].tls.caCertificates}"

	// Path for ConfigPatch in envoyFilter
	// Required parameters: envoyFilter config patch index
	EnvoyFilterConfigPath = "{.spec.configPatches[%d].patch.value}"
)

// ErrorLine returns the line number of the input path key in the resource
func ErrorLine(r *resource.Instance, path string) (line int, found bool) {
	fieldMap := r.Origin.FieldMap()
	line, ok := fieldMap[path]
	if !ok {
		return 0, false
	}
	return line, true
}

// ExtractLabelFromSelectorString returns the label of the match in the k8s labels.Selector
func ExtractLabelFromSelectorString(s string) string {
	equalIndex := strings.Index(s, "=")
	if equalIndex < 0 {
		return ""
	}
	return s[:equalIndex]
}

func AddLineNumber(r *resource.Instance, ann string, m diag.Message) bool {
	if line, ok := ErrorLine(r, fmt.Sprintf(Annotation, ann)); ok {
		m.Line = line
		return true
	}
	return false
}
