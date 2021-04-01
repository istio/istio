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
	"path/filepath"

	"istio.io/istio/prow/asm/tester/pkg/exec"
	"istio.io/istio/prow/asm/tester/pkg/resource"
)

func Teardown(settings *resource.Settings) error {
	log.Println("🎬 start tearing down the environment...")

	// TODO: convert the script into Go
	teardownEnvScript := filepath.Join(settings.RepoRootDir, "prow/asm/tester/scripts/teardown-env.sh")
	if err := exec.Run(teardownEnvScript); err != nil {
		return fmt.Errorf("error tearing down the environment: %w", err)
	}

	return nil
}
