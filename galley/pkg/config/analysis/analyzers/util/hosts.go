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
	"regexp"
	"strings"

	"istio.io/istio/galley/pkg/config/resource"
)

var (
	fqdnPattern = regexp.MustCompile(`^(.+)\.(.+)\.svc\.cluster\.local$`)
)

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

func ConvertHostToFQDN(host, namespace string) string {
	if (host == "") || (namespace == "") {
		return ""
	} else if strings.HasPrefix(host, "*") {
		return host
	} else if strings.Contains(host, ".") {
		return host
	} else {
		// need to return Fully Qualified Domain Name
		return host + "." + namespace + "." + DefaultKubernetesDomain
	}
}
