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

// FinedErrorLineNum returns the error line number with the annotation name and the field map from resource.Origin as the input
func ErrorLineForAnnotation(annotationName string, r *resource.Instance) int {
	path := "{.metadata.annotations"
	path += "." + annotationName + "}"
	return FindErrorLine(path, r.Origin.GetFieldMap())
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
