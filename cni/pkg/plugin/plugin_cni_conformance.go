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

package plugin

import (
	"testing"

	"github.com/containernetworking/cni/pkg/types"
)

// Validate k8sArgs struct works for unmarshalling kubelet args
// This is important for CNI plugin conformance
func TestLoadArgs(t *testing.T) {
	kubeletArgs := "IgnoreUnknown=1;K8S_POD_NAMESPACE=istio-system;" +
		"K8S_POD_NAME=istio-sidecar-injector-8489cf78fb-48pvg;" +
		"K8S_POD_INFRA_CONTAINER_ID=3c41e946cf17a32760ff86940a73b06982f1815e9083cf2f4bfccb9b7605f326"

	k8sArgs := K8sArgs{}
	if err := types.LoadArgs(kubeletArgs, &k8sArgs); err != nil {
		t.Fatalf("LoadArgs failed with error: %v", err)
	}

	if string(k8sArgs.K8S_POD_NAMESPACE) == "" || string(k8sArgs.K8S_POD_NAME) == "" {
		t.Fatalf("LoadArgs didn't convert args properly, K8S_POD_NAME=\"%s\";K8S_POD_NAMESPACE=\"%s\"",
			string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE))
	}
}
