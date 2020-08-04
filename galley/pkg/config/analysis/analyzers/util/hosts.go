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
	"strings"

	"istio.io/istio/pkg/config/resource"
)

type ScopedFqdn string

// GetScopeAndFqdn splits ScopedFqdn back to scope namespace and fqdn parts
func (s ScopedFqdn) GetScopeAndFqdn() (string, string) {
	parts := strings.SplitN(string(s), "/", 2)
	return parts[0], parts[1]
}

// InScopeOf returns true if ns is in the scope of ScopedFqdn
func (s ScopedFqdn) InScopeOf(ns string) bool {
	scope, fqdn := s.GetScopeAndFqdn()
	fn := GetFullNameFromFQDN(fqdn)
	return scope == "*" || scope == "." && ns == fn.Namespace.String() || scope == ns
}

// NewScopedFqdn converts the passed host to FQDN if needed and applies the passed scope.
func NewScopedFqdn(scope string, namespace resource.Namespace, host string) ScopedFqdn {
	fqdn := ConvertHostToFQDN(namespace, host)
	return ScopedFqdn(scope + "/" + fqdn)
}

// GetResourceNameFromHost figures out the resource.FullName to look up from the provided host string
// We need to handle two possible formats: short name and FQDN
// https://istio.io/docs/reference/config/networking/v1alpha3/virtual-service/#Destination
func GetResourceNameFromHost(defaultNamespace resource.Namespace, host string) resource.FullName {

	// First, try to parse as FQDN (which can be cross-namespace)
	name := GetFullNameFromFQDN(host)

	// Otherwise, treat this as a short name and use the assumed namespace
	if name.Namespace == "" {
		name.Namespace = defaultNamespace
		name.Name = resource.LocalName(host)
	}
	return name
}

// GetFullNameFromFQDN tries to parse namespace and name from a fqdn.
// Empty strings are returned if either namespace or name cannot be parsed.
func GetFullNameFromFQDN(fqdn string) resource.FullName {
	result := fqdnPattern.FindAllStringSubmatch(fqdn, -1)
	if len(result) == 0 {
		return resource.FullName{
			Namespace: "",
			Name:      "",
		}
	}
	return resource.FullName{
		Namespace: resource.Namespace(result[0][2]),
		Name:      resource.LocalName(result[0][1]),
	}
}

// ConvertHostToFQDN returns the given host as a FQDN, if it isn't already.
func ConvertHostToFQDN(namespace resource.Namespace, host string) string {
	fqdn := host
	// Convert to FQDN only if host is not a wildcard or a FQDN
	if !strings.HasPrefix(host, "*") &&
		!strings.Contains(host, ".") {
		fqdn = host + "." + string(namespace) + "." + DefaultKubernetesDomain
	}
	return fqdn
}
