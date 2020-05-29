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

package istioagent

import (
	"testing"
)

// Validate that SDSAgent comes up without errors when configured with file mounted certs.
func TestSDSAgentWithFileMountedCerts(t *testing.T) {
	fm := fileMountedCertsEnv
	fileMountedCertsEnv = true
	defer func() { fileMountedCertsEnv = fm }()
	// Validate that SDS server can start without any error.
	sa := NewSDSAgent("istiod.istio-system:15012", false, "custom", "", "", "kubernetes")
	_, err := sa.Start(true, "test")
	if err != nil {
		t.Fatalf("Unexpected error starting SDSAgent %v", err)
	}
}
