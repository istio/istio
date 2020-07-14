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

package dispatcher

import (
	"istio.io/istio/mixer/pkg/attribute"
)

// getIdentityNamespace returns the namespace scope for the attribute bag
func getIdentityNamespace(attrs attribute.Bag) (namespace string) {
	reporterType := "inbound"
	if typeValue, ok := attrs.Get("context.reporter.kind"); ok {
		if v, isString := typeValue.(string); isString {
			reporterType = v
		}
	}
	// use destination.namespace for inbound, source.namespace for outbound
	switch reporterType {
	case "inbound":
		destinationNamespace, ok := attrs.Get("destination.namespace")
		if ok {
			namespace, _ = destinationNamespace.(string)
		}
	case "outbound":
		sourceNamespace, ok := attrs.Get("source.namespace")
		if ok {
			namespace, _ = sourceNamespace.(string)
		}
	}

	return
}
