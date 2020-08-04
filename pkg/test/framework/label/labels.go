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

	// Flaky indicates that a test is currently flaky and should not be run as part
	// of presubmit or postsubmit. When a test is determined to be Flaky, a github
	// issue should be created to fix the test.
	Flaky Instance = "flaky"

	// Multicluster indicates that the test requires a multicluster configuration.
	Multicluster Instance = "multicluster"
)

var all = NewSet(
	Postsubmit,
	CustomSetup,
	Flaky,
	Multicluster)

// Find the label with the given name
func Find(name string) (Instance, bool) {
	candidate := Instance(name)
	if _, ok := all[candidate]; ok {
		return candidate, true
	}

	return Instance(""), false
}
