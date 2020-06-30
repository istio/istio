// +build !race

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

package kubernetesenv

import (
	"context"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/secretcontroller"
)

// This test is skipped by the build tag !race due to https://github.com/istio/istio/issues/15610
func Test_KubeSecretController(t *testing.T) {
	secretcontroller.BuildClientsFromConfig = func(kubeConfig []byte) (kube.Client, error) {
		return kube.NewFakeClient(), nil
	}
	clientset := fake.NewSimpleClientset()
	b := newBuilder(func(string, adapter.Env) (kubernetes.Interface, error) {
		return clientset, nil
	})

	// Call kube Build function which will start the secret controller.
	// Sleep to allow secret process to start.
	_, err := b.Build(context.Background(), test.NewEnv(t))
	if err != nil {
		t.Fatalf("error building adapter: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	// Create the multicluster secret.
	err = createMultiClusterSecret(clientset)
	if err != nil {
		t.Fatalf("Unexpected error on secret create: %v", err)
	}

	// Test - Verify that the remote controller has been added.
	verifyControllers(t, b, 2, "create remote controller")

	// Delete the mulicluster secret.
	err = deleteMultiClusterSecret(clientset)
	if err != nil {
		t.Fatalf("Unexpected error on secret delete: %v", err)
	}

	// Test - Verify that the remote controller has been removed.
	verifyControllers(t, b, 1, "delete remote controller")
}
