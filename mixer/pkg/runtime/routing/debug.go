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

package routing

// tableDebugInfo contains debugging information for the table.
type tableDebugInfo struct {
	// match condition sets by the input set id.
	matchesByID map[uint32]string

	// instanceName set of builders by the input set.
	instanceNamesByID map[uint32][]string
}
