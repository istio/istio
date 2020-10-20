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

package metrics

import (
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/operator/pkg/util"
)

// resourceCounts keeps track of the number of resources owned by each
// IstioOperator resource. The reported metric is the sum across all these.
type resourceCounts struct {
	mu        *sync.Mutex
	resources map[schema.GroupKind]map[string]struct{}
}

var rc *resourceCounts

func initOperatorCrdResourceMetrics() {
	rc = &resourceCounts{
		mu:        &sync.Mutex{},
		resources: map[schema.GroupKind]map[string]struct{}{},
	}
}

// AddResource adds the resource of given kind to the set of owned objects
func AddResource(name string, gk schema.GroupKind) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if _, present := rc.resources[gk]; !present {
		rc.resources[gk] = map[string]struct{}{}
	}
	rc.resources[gk][name] = struct{}{}
}

// RemoveResource removes the resource of given kind to the set of owned objects
func RemoveResource(name string, gk schema.GroupKind) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	delete(rc.resources[gk], name)
}

// ReportOwnedResourceCounts reports the owned resource count
// metric by Group and Kind.
func ReportOwnedResourceCounts() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	for gk, r := range rc.resources {
		OwnedResourceTotal.
			With(ResourceKindLabel.Value(util.GKString(gk))).
			Record(float64(len(r)))
	}
}
