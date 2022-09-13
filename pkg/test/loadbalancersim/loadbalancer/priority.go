//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package loadbalancer

import (
	mesh2 "istio.io/istio/pkg/test/loadbalancersim/mesh"
)

type PrioritySelector func(src *mesh2.Client, dest *mesh2.Node) uint32

func LocalityPrioritySelector(src *mesh2.Client, dest *mesh2.Node) uint32 {
	priority := uint32(2)
	if src.Locality().Region == dest.Locality().Region {
		priority = 1
		if src.Locality().Zone == dest.Locality().Zone {
			priority = 0
		}
	}
	return priority
}
