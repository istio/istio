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

package util

import (
	"strings"

	"istio.io/istio/galley/pkg/config/resource"
)

type ScopedFqdn string

// GetResourceNameFromHost figures out the resource.Name to look up from the provided host string
// We need to handle two possible formats: short name and FQDN
// https://istio.io/docs/reference/config/networking/v1alpha3/virtual-service/#Destination
func GetResourceNameFromHost(defaultNamespace, host string) resource.Name {

	// First, try to parse as FQDN (which can be cross-namespace)
	namespace, name := getNamespaceAndNameFromFQDN(host)

	//Otherwise, treat this as a short name and use the assumed namespace
	if namespace == "" {
		namespace = defaultNamespace
		name = host
	}
	return resource.NewName(namespace, name)
}

func getNamespaceAndNameFromFQDN(fqdn string) (string, string) {
	result := fqdnPattern.FindAllStringSubmatch(fqdn, -1)
	if len(result) == 0 {
		return "", ""
	}
	return result[0][2], result[0][1]
}

// GetScopedFqdnHostname converts the passed host to FQDN if needed and applies the passed scope.
func GetScopedFqdnHostname(scope, namespace, host string) ScopedFqdn {
	name := convertHostToFQDN(namespace, host)
	return ScopedFqdn(scope + "/" + name)
}

func convertHostToFQDN(namespace, host string) string {
	fqdn := host
	// Convert to FQDN only if host is not a wildcard or a FQDN
	if !strings.HasPrefix(host, "*") &&
		!strings.Contains(host, ".") {
		fqdn = host + "." + namespace + "." + DefaultKubernetesDomain
	}
	return fqdn
}
