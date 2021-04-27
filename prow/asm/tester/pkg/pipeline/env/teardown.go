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

package env

import (
	"fmt"
	"log"

	"istio.io/istio/prow/asm/tester/pkg/exec"
	"istio.io/istio/prow/asm/tester/pkg/resource"
)

func Teardown(settings *resource.Settings) error {
	log.Println("🎬 start tearing down the environment...")

	if err := removePermissions(settings); err != nil {
		return fmt.Errorf("error removing gcp permissions: %w", err)
	}

	return nil
}

func removePermissions(settings *resource.Settings) error {
	if settings.ClusterType == resource.GKEOnGCP && settings.ControlPlane == resource.Unmanaged {
		return removeGcpPermissions(settings)
	}
	return nil
}

func removeGcpPermissions(settings *resource.Settings) error {
	for _, projectId := range settings.GCPProjects {
		if projectId != settings.GCRProject {
			projectNum, err := getProjectNumber(projectId)
			if err != nil {
				return err
			}
			err = exec.Run(
				fmt.Sprintf("gcloud projects remove-iam-policy-binding %s "+
					"--member=serviceAccount:%s-compute@developer.gserviceaccount.com "+
					"--role=roles/storage.objectViewer",
					settings.GCRProject,
					projectNum),
			)
			if err != nil {
				return fmt.Errorf("error removing the binding for the service account to access GCR: %w", err)
			}
		}
	}
	return nil
}
