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

package kube

import (
	"fmt"
	"strings"

	"istio.io/istio/prow/asm/tester/pkg/exec"
)

func GetEnvironProjectID(kubeConfig string) (string, error) {
	configs := strings.Split(kubeConfig, ":")
	environProjectId, err := exec.RunWithOutput(
		fmt.Sprintf(`bash -c 'kubectl get memberships.hub.gke.io membership -o=json --kubeconfig=%s | jq .spec.workload_identity_pool | sed "s/^\"\(.*\).\(svc\|hub\).id.goog\"$/\1/g"'`, configs[0]))
	if err != nil {
		err = fmt.Errorf("error getting the Environ Project ID for kubeconfig %s: %w", configs[0], err)
	}
	return strings.TrimSpace(environProjectId), err
}