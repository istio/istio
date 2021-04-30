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

package userauth

import (
	"fmt"

	"istio.io/istio/prow/asm/tester/pkg/exec"
	"istio.io/istio/prow/asm/tester/pkg/resource"
)

func Teardown(settings *resource.Settings) error {
	exec.Run("bash -c 'kill -9 $(pgrep -f \"kubectl port-forward\")'")

	if err := cleanupASMUserAuth(settings); err != nil {
		return fmt.Errorf("error cleaning up user auth: %w", err)
	}
	if err := cleanupUserAuthDependencies(settings); err != nil {
		return fmt.Errorf("error cleaning up user auth dependencies: %w", err)
	}

	return nil
}

// Cleanup ASM User Auth manifest.
func cleanupASMUserAuth(settings *resource.Settings) error {
	cmds := []string{
		fmt.Sprintf("kubectl delete -f %s/user-auth/pkg/asm_user_auth_config_v1alpha1.yaml", settings.ConfigDir),
		fmt.Sprintf("kubectl delete -f %s/user-auth/pkg/cluster_role_binding.yaml", settings.ConfigDir),
		fmt.Sprintf("kubectl delete -f %s/user-auth/pkg/ext_authz.yaml", settings.ConfigDir),
		"kubectl -n userauth-test delete -f https://raw.githubusercontent.com/istio/istio/master/samples/httpbin/httpbin.yaml",
		fmt.Sprintf("rm -rf %s/user-auth/pkg", settings.ConfigDir),
		"kubectl delete ns asm-user-auth userauth-test",
	}
	return exec.RunMultiple(cmds)
}

// Cleanup dependencies
func cleanupUserAuthDependencies(settings *resource.Settings) error {
	return exec.Run(fmt.Sprintf("rm -rf %s/user-auth/dependencies", settings.ConfigDir))
}
