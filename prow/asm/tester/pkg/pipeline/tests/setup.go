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

package tests

import (
	"fmt"
	"istio.io/istio/prow/asm/tester/pkg/exec"
	"istio.io/istio/prow/asm/tester/pkg/resource"
	"istio.io/istio/prow/asm/tester/pkg/tests"
	"log"
	"path/filepath"
)

func Setup(settings *resource.Settings) error {
	log.Println("🎬 start running the setups for the tests...")

	if err := tests.Setup(settings); err != nil {
		return fmt.Errorf("error setting up the tests: %w", err)
	}

	// Run the setup-tests.sh
	// TODO: convert the script into Go
	setupTestsScript := filepath.Join(settings.RepoRootDir, "prow/asm/tester/scripts/setup-tests.sh")
	if err := exec.Run(setupTestsScript); err != nil {
		return fmt.Errorf("error setting up the tests in setup-tests.sh: %w", err)
	}

	if settings.ControlPlane == string(resource.Unmanaged) && settings.FeatureToTest == "USER_AUTH" {
		// TODO(b/182912549): port-forward in go test code instead of here
		go exec.Run("kubectl port-forward service/istio-ingressgateway 8443:443 -n istio-system")
	}

	return nil
}
