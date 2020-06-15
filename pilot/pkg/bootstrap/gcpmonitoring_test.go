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
package bootstrap

import (
	"os"
	"testing"
)

// ASM monitoring relies on these two env vars. Add these unit test to prevent breakage from OSS.
func TestPodEnvVar(t *testing.T) {
	os.Setenv("POD_NAME", "pod_name")
	if podNameVar.Get() != "pod_name" {
		t.Errorf("POD_NAME env want pod_name, got %v", podNameVar.Get())
	}
}

func TestPodNamespaceEnvVar(t *testing.T) {
	os.Setenv("POD_NAMESPACE", "pod_namespace")
	if PodNamespaceVar.Get() != "pod_namespace" {
		t.Errorf("POD_NAMESPACE env want pod_namespace, got %v", PodNamespaceVar.Get())
	}
}
