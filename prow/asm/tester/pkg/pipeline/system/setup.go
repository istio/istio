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

package system

import (
	"fmt"
	"log"
	"path/filepath"

	"istio.io/istio/prow/asm/tester/pkg/exec"
	"istio.io/istio/prow/asm/tester/pkg/resource"
)

func Setup(settings *resource.Settings) error {
	log.Println("🎬 start installing ASM control plane...")

	// TODO: convert the script into Go
	installASMScript := filepath.Join(settings.RepoRootDir, "prow/asm/tester/scripts/setup-asm.sh")
	if err := exec.Run(installASMScript); err != nil {
		return fmt.Errorf("error installing the ASM control plane: %w", err)
	}

	return nil
}
