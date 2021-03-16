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

package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"istio.io/istio/prow/asm/tester/pkg/resource"
)

func (apt *ASMPipelineTester) SetupEnv() error {
	fmt.Println("🎬 start setting up the environment...")

	// Validate the settings before proceeding.
	if err := resource.ValidateSettings(&apt.Settings); err != nil {
		return err
	}

	// TODO(chizhg): delete all the env var injections after we convert all the
	// bash to Go and remove all the env var dependencies.
	os.Setenv("CONTROL_PLANE", apt.ControlPlane)
	os.Setenv("CA", apt.CA)
	os.Setenv("WIP", apt.WIP)
	os.Setenv("REVISION_CONFIG_FILE", apt.RevisionConfig)
	os.Setenv("TEST_TARGET", apt.TestTarget)
	os.Setenv("DISABLED_TESTS", apt.DisabledTests)

	os.Setenv("USE_VM", strconv.FormatBool(apt.UseVMs))
	os.Setenv("STATIC_VMS", apt.VMStaticConfigDir)
	os.Setenv("VM_DISTRO", apt.VMImageFamily)
	os.Setenv("IMAGE_PROJECT", apt.VMImageProject)

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("error getting the current working directory: %w", err)
	}
	os.Setenv("CONFIG_DIR", filepath.Join(wd, "prow/asm/tester/configs"))

	return nil
}

func (apt *ASMPipelineTester) TeardownEnv() error {
	fmt.Println("TODO(chizhg): tear down env...")
	return nil
}
