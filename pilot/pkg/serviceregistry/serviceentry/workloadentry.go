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

package serviceentry

import (
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/network"
)

// return the mesh network for the workload entry. Empty string if not found.
func (s *Controller) workloadEntryNetwork(wle *networking.WorkloadEntry) network.ID {
	if s == nil {
		return ""
	}
	// 1. first check the wle.Network
	if wle.Network != "" {
		return network.ID(wle.Network)
	}

	// 2. fall back to the passed in getNetworkCb func.
	if s.networkIDCallback != nil {
		return s.networkIDCallback(wle.Address, wle.Labels)
	}
	return ""
}
