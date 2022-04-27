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

package cleanup

// Strategy enumerates the options for configuration cleanup behavior.
type Strategy int

const (
	// Always will trigger a cleanup operation the test context completes.
	Always Strategy = iota

	// Conditionally will trigger a cleanup operation the test context
	// completes, unless -istio.test.nocleanup is set.
	Conditionally

	// None will not configure a cleanup operation to be performed when the
	// context terminates.
	None
)
