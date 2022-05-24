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

package label

const (
	// Postsubmit indicates that the test should be run as part of a postsubmit run only.
	Postsubmit Instance = "postsubmit"

	// CustomSetup indicates that the test requires a custom Istio installation.
	CustomSetup Instance = "customsetup"

	// IPv4 indicates a test is only compatible with IPv4 clusters.
	// Any usage of this should have an associated GitHub issue to make it compatible with IPv6
	IPv4 Instance = "ipv4"
)

var all = NewSet(
	Postsubmit,
	CustomSetup,
	IPv4)

// Find the label with the given name
func Find(name string) (Instance, bool) {
	candidate := Instance(name)
	if _, ok := all[candidate]; ok {
		return candidate, true
	}

	return "", false
}
