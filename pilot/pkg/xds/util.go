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

package xds

import (
	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/labels"
)

// getSubSetLabels returns the labels associated with a subset of a given service.
func getSubSetLabels(dr *networkingapi.DestinationRule, subsetName string) labels.Instance {
	// empty subset
	if subsetName == "" {
		return nil
	}

	if dr == nil {
		return nil
	}

	for _, subset := range dr.Subsets {
		if subset.Name == subsetName {
			if len(subset.Labels) == 0 {
				return nil
			}
			return subset.Labels
		}
	}

	return nil
}
