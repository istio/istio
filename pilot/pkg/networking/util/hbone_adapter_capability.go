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
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/istio/pkg/util/sets"
)

// Proxy extensions HBONE CONNECT wait requires.
var HBONEConnectWaitExtensions = sets.New(HBONEOriginationAdapterTransport, HBONERejectionPool)

func NodeSupportsHBONEConnectWait(node *core.Node) bool {
	foundExtensions := sets.New[string]()
	for _, ext := range node.GetExtensions() {
		if !ext.GetDisabled() && HBONEConnectWaitExtensions.Contains(ext.GetName()) {
			foundExtensions.Insert(ext.GetName())
		}
	}
	return foundExtensions.Len() == HBONEConnectWaitExtensions.Len()
}
