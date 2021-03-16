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

	"sigs.k8s.io/kubetest2/pkg/exec"
)

func (apt *ASMPipelineTester) SetupTests() error {
	fmt.Println("TODO(chizhg): setup tests...")
	return nil
}

func (apt *ASMPipelineTester) RunTests() error {
	fmt.Println("🎬 start running the tests...")
	wd, _ := os.Getwd()
	testScript := filepath.Join(wd, "prow/asm/tester/scripts/run-tests.sh")
	cmd := exec.Command(testScript)
	cmd.SetEnv(os.Environ()...)
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error running the tests: %w", err)
	}

	return nil
}

func (apt *ASMPipelineTester) TeardownTests() error {
	fmt.Println("TODO(chizhg): tear down tests...")
	return nil
}
