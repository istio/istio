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
	"log"
	"os"
	"path/filepath"
	"strings"

	"istio.io/istio/prow/asm/tester/pkg/exec"
	"istio.io/istio/prow/asm/tester/pkg/resource"
)

func Setup(settings *resource.Settings) error {
	log.Println("🎬 start running the setups for the tests...")

	gcrProjectID1 := settings.GCRProject
	var gcrProjectID2 string
	if len(settings.GCPProjects) == 2 {
		// If it's using multiple gke clusters, set gcrProjectID2 as the project
		// for the second cluster.
		gcrProjectID2 = settings.GCPProjects[1]
	} else {
		gcrProjectID2 = gcrProjectID1
	}
	// When HUB Workload Identity Pool is used in the case of multi projects setup, clusters in different projects
	// will use the same WIP and P4SA of the Hub host project.
	if settings.WIP == string(resource.HUB) && strings.Contains(settings.TestTarget, "security") {
		gcrProjectID2 = gcrProjectID1
	}

	for name, val := range map[string]string{
		// exported GCR_PROJECT_ID_1 and GCR_PROJECT_ID_2
		// for security and telemetry test.
		"GCR_PROJECT_ID_1": gcrProjectID1,
		"GCR_PROJECT_ID_2": gcrProjectID2,
	} {
		log.Printf("Set env var: %s=%s", name, val)
		if err := os.Setenv(name, val); err != nil {
			return fmt.Errorf("error setting env var %q to %q", name, val)
		}
	}

	// Run the setup-tests.sh
	// TODO: convert the script into Go
	setupTestsScript := filepath.Join(settings.RepoRootDir, "prow/asm/tester/scripts/setup-tests.sh")
	if err := exec.Run(setupTestsScript); err != nil {
		return fmt.Errorf("error setting up the tests: %w", err)
	}

	if settings.ControlPlane == string(resource.Unmanaged) && settings.FeatureToTest == "USER_AUTH" {
		// TODO(b/182912549): port-forward in go test code instead of here
		go exec.Run("kubectl port-forward service/istio-ingressgateway 8443:443 -n istio-system")
	}

	return nil
}
