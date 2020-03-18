// Copyright 2020 Istio Authors
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

package istiocontrolplane

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/api/operator/v1alpha1"
	v1alpha11 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
)

var _ = Describe("IstioControlPlane Controller", func() {

	const timeout = time.Second * 200
	const interval = time.Second * 1

	BeforeEach(func() {
		// Add any setup steps that needs to be executed before each test )
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
	})

	Context("Create Controller for Default profile", func() {
		It("Should create successfully", func() {

			key := types.NamespacedName{
				Name:      "istio-controlplane",
				Namespace: "istio-operator",
			}

			created := &v1alpha11.IstioOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Kind:       "IstioOperator",
				ApiVersion: "install.istio.io/v1alpha1",
				Spec: &v1alpha1.IstioOperatorSpec{
					Profile: "default",
					MeshConfig: &mesh.MeshConfig{
						RootNamespace: "istio-system",
					},
				},
			}

			// Create
			Expect(k8sClient.Create(context.Background(), created)).Should(Succeed())

			By("Expecting Healthy Status")
			Eventually(func() *v1alpha1.InstallStatus {
				f := &v1alpha11.IstioOperator{}
				k8sClient.Get(context.Background(), key, f)
				return f.Status
			}, timeout, interval).Should(Equal(v1alpha1.InstallStatus_HEALTHY))

			// Update
			updated := &v1alpha11.IstioOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Kind:       "IstioOperator",
				ApiVersion: "install.istio.io/v1alpha1",
				Spec: &v1alpha1.IstioOperatorSpec{Profile: "demo"},
			}
			Expect(k8sClient.Update(context.Background(), updated)).Should(Succeed())

			By("Expecting new spec")
			Eventually(func() *v1alpha1.InstallStatus {
				f := &v1alpha11.IstioOperator{}
				k8sClient.Get(context.Background(), key, f)
				return f.Status
			}, timeout, interval).Should(Equal(v1alpha1.InstallStatus_HEALTHY))

			// Delete
			By("Expecting to delete successfully")
			Eventually(func() error {
				f := &v1alpha11.IstioOperator{}
				k8sClient.Get(context.Background(), key, f)
				return k8sClient.Delete(context.Background(), f)
			}, timeout, interval).Should(Succeed())
		})
	})
})
