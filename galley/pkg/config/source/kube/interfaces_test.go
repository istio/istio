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

package kube_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"k8s.io/client-go/rest"

	"istio.io/istio/galley/pkg/config/source/kube"
)

func TestCreateConfig(t *testing.T) {
	k := kube.NewInterfaces(&rest.Config{})

	if _, err := k.DynamicInterface(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if _, err := k.APIExtensionsClientset(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if _, err := k.KubeClient(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestNewKubeWithInvalidConfigFileShouldFail(t *testing.T) {
	g := NewGomegaWithT(t)
	_, err := kube.NewInterfacesFromConfigFile("badconfigfile")
	g.Expect(err).ToNot(BeNil())
}

func TestNewKube(t *testing.T) {
	// Should not panic
	_ = kube.NewInterfaces(&rest.Config{})
}
