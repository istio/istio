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
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"

	"istio.io/istio/pkg/util/sets"
)

var SupportedFeatures = suite.AllFeatures

var gatewaySupportedFeatures = getSupportedFeatures()

func getSupportedFeatures() []k8sv1.SupportedFeature {
	ret := sets.New[k8sv1.SupportedFeature]()
	for _, feature := range SupportedFeatures.UnsortedList() {
		ret.Insert(k8sv1.SupportedFeature(feature))
	}
	return sets.SortedList(ret)
}
