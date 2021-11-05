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

package plugin

import (
	"testing"
)

func TestMain(m *testing.M) {
	// Leak test is explicitly disable for this test. The test involves an async job that calls GCE
	// metadata server code against a fake metadata server. We do not control the client, and cannot
	// configure it to exit early, retry faster, etc - its all fixed. As a result, we don't have a good
	// way to shut it down if it is still retrying in the background.
}
