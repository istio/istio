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

// ContextStr returns the kubectl contexts name.
func ContextStr() (string, error) {
	// Get all contexts of the clusters.
	var kubectlContexts string
	var err error
	kubectlContexts, err = exec.RunWithOutput("kubectl config view -o jsonpath=\"{range .contexts[*]}{.name}{','}{end}\"")
	if err != nil {
		return "", fmt.Errorf("error getting the kubectl contexts: %w", err)
	}
	// Trim the trailing ","
	kubectlContexts = kubectlContexts[:len(kubectlContexts)-1]
	return kubectlContexts, nil
}

func ContextArr(contexts string) string {
	return strings.Join(strings.Split(contexts, ","), " ")
}
