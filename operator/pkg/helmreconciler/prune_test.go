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

package helmreconciler

import (
	"context"
	_ "embed"
	"os"
	"path/filepath"
	"sync"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	"istio.io/api/label"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/assert"
)

const (
	testRevision = "test"
)

var (
	//go:embed testdata/iop-test-gw-1.yaml
	iopTestGwData1 []byte
	//go:embed testdata/iop-test-gw-2.yaml
	iopTestGwData2 []byte
)

func TestHelmReconciler_DeleteControlPlaneByManifest(t *testing.T) {
	t.Run("deleteControlPlaneByManifest", func(t *testing.T) {
		cl := fake.NewClientBuilder().Build()
		df := filepath.Join(env.IstioSrc, "manifests/profiles/default.yaml")
		iopStr, err := os.ReadFile(df)
		if err != nil {
			t.Fatal(err)
		}
		iop := &v1alpha1.IstioOperator{}
		if err := yaml.UnmarshalStrict(iopStr, iop); err != nil {
			t.Fatal(err)
		}
		iop.Spec.Revision = testRevision
		iop.Spec.InstallPackagePath = filepath.Join(env.IstioSrc, "manifests")

		h := &HelmReconciler{
			client:     cl,
			kubeClient: kube.NewFakeClientWithVersion("24"),
			opts: &Options{
				ProgressLog: progress.NewLog(),
				Log:         clog.NewDefaultLogger(),
			},
			iop:           iop,
			countLock:     &sync.Mutex{},
			prunedKindSet: map[schema.GroupKind]struct{}{},
		}
		manifestMap, err := h.RenderCharts()
		if err != nil {
			t.Fatalf("failed to render manifest: %v", err)
		}
		applyResourcesIntoCluster(t, h, manifestMap)
		if err := h.DeleteControlPlaneByManifests(manifestMap, testRevision, false); err != nil {
			t.Fatalf("HelmReconciler.DeleteControlPlaneByManifests() error = %v", err)
		}
		for _, gvk := range append(h.NamespacedResources(), ClusterCPResources...) {
			receiver := &unstructured.Unstructured{}
			receiver.SetGroupVersionKind(schema.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind})
			objKey := client.ObjectKey{Namespace: "istio-system", Name: "istiod-test"}
			if gvk.Kind == name.MutatingWebhookConfigurationStr {
				objKey.Name = "istio-sidecar-injector-test"
			}
			// List does not work well here as that requires adding all resource types to the fake client scheme
			if err := h.client.Get(context.TODO(), objKey, receiver); err != nil {
				// the error is expected because we expect resources do not exist any more in the cluster
				t.Logf(err.Error())
			}
			obj := receiver.Object
			if obj["spec"] != nil {
				t.Errorf("got resource: %s/%s from the cluster, expected to be deleted", receiver.GetKind(), receiver.GetName())
			}
		}
	})
}

func applyResourcesIntoCluster(t *testing.T, h *HelmReconciler, manifestMap name.ManifestMap) {
	for cn, ms := range manifestMap.Consolidated() {
		objects, err := object.ParseK8sObjectsFromYAMLManifest(ms)
		if err != nil {
			t.Fatalf("failed parse k8s objects from yaml: %v", err)
		}
		for _, obj := range objects {
			obju := obj.UnstructuredObject()
			if err := h.applyLabelsAndAnnotations(obju, cn); err != nil {
				t.Errorf("failed to apply label and annotations: %v", err)
			}
			if err := h.ApplyObject(obj.UnstructuredObject(), false); err != nil {
				t.Errorf("HelmReconciler.ApplyObject() error = %v", err)
			}
		}
	}
}

func TestHelmReconciler_GetPrunedResources(t *testing.T) {
	t.Run("get gateway pruned resources", func(t *testing.T) {
		var h1 *HelmReconciler
		cl := fake.NewClientBuilder().Build()
		// init two custom gateways with revision
		gateways := [][]byte{iopTestGwData1, iopTestGwData2}
		for i, data := range gateways {
			iop := &v1alpha1.IstioOperator{}
			err := yaml.UnmarshalStrict(data, iop)
			assert.NoError(t, err)
			h := &HelmReconciler{
				client:     cl,
				kubeClient: kube.NewFakeClientWithVersion("24"),
				opts: &Options{
					ProgressLog: progress.NewLog(),
					Log:         clog.NewDefaultLogger(),
				},
				iop:           iop,
				countLock:     &sync.Mutex{},
				prunedKindSet: map[schema.GroupKind]struct{}{},
			}
			if i == 0 {
				h1 = h
			}
			manifestMap, err := h.RenderCharts()
			if err != nil {
				t.Fatalf("failed to render manifest: %v", err)
			}
			applyResourcesIntoCluster(t, h, manifestMap)
		}
		// delete one iop: iop-test-gw-1, get its pruned resources
		componentName := string(name.IngressComponentName)
		resources, err := h1.GetPrunedResources(h1.iop.Spec.Revision, false, componentName)
		assert.NoError(t, err)
		assert.Equal(t, true, len(resources) > 0)
		// check resources, only associated with iop-test-gw-1 istiooperator CR,
		// otherwise, the resources of all IngressGateways components will be deleted.
		// See https://github.com/istio/istio/issues/40577 for more details.
		for _, uslist := range resources {
			for _, u := range uslist.Items {
				assert.Equal(t, h1.iop.Spec.Revision, u.GetLabels()[label.IoIstioRev.Name])
				assert.Equal(t, componentName, u.GetLabels()[IstioComponentLabelStr])
				assert.Equal(t, h1.iop.GetName(), u.GetLabels()[OwningResourceName])
				assert.Equal(t, h1.iop.GetNamespace(), u.GetLabels()[OwningResourceNamespace])
			}
		}
	})
}

func TestPilotExist(t *testing.T) {
	t.Run("exist", func(t *testing.T) {
		cl := fake.NewClientBuilder().Build()
		iop := &v1alpha1.IstioOperator{}
		h := &HelmReconciler{
			client:     cl,
			kubeClient: kube.NewFakeClientWithVersion("24"),
			opts: &Options{
				ProgressLog: progress.NewLog(),
				Log:         clog.NewDefaultLogger(),
			},
			iop:           iop,
			countLock:     &sync.Mutex{},
			prunedKindSet: map[schema.GroupKind]struct{}{},
		}
		mockClient := kube.NewFakeClient(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istiod",
				Namespace: "istio-system",
				Labels:    map[string]string{"app": "istiod"},
			},
		})

		if exist, err := h.pilotExists(mockClient, "istio-system"); err != nil {
			t.Fatalf("HelmReconciler.pilotExists error = %v", err)
		} else if !exist {
			t.Errorf("HelmReconciler.pilotExists fail")
		}
	})

	t.Run("non-exist", func(t *testing.T) {
		cl := fake.NewClientBuilder().Build()
		iop := &v1alpha1.IstioOperator{}
		kc := kube.NewFakeClientWithVersion("24")
		h := &HelmReconciler{
			client:     cl,
			kubeClient: kc,
			opts: &Options{
				ProgressLog: progress.NewLog(),
				Log:         clog.NewDefaultLogger(),
			},
			iop:           iop,
			countLock:     &sync.Mutex{},
			prunedKindSet: map[schema.GroupKind]struct{}{},
		}
		if exist, err := h.pilotExists(kc, "istio-system"); err != nil {
			t.Fatalf("HelmReconciler.pilotExists error = %v", err)
		} else if exist {
			t.Errorf("HelmReconciler.pilotExists fail")
		}
	})
}
