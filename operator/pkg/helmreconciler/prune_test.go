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
	"io/ioutil"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	"istio.io/istio/pkg/test/env"
)

const (
	testRevision = "test"
)

func TestHelmReconciler_DeleteControlPlaneByManifest(t *testing.T) {
	t.Run("deleteControlPlaneByManifest", func(t *testing.T) {
		s := runtime.NewScheme()
		cl := fake.NewFakeClientWithScheme(s)
		df := filepath.Join(env.IstioSrc, "manifests/profiles/default.yaml")
		iopStr, err := ioutil.ReadFile(df)
		if err != nil {
			t.Fatal(err)
		}
		iop := &v1alpha1.IstioOperator{}
		if err := util.UnmarshalWithJSONPB(string(iopStr), iop, false); err != nil {
			t.Fatal(err)
		}
		iop.Spec.Revision = testRevision
		iop.Spec.InstallPackagePath = filepath.Join(env.IstioSrc, "manifests")

		h := &HelmReconciler{client: cl, opts: &Options{ProgressLog: progress.NewLog(), Log: clog.NewDefaultLogger()}, iop: iop}
		manifestMap, err := h.RenderCharts()
		if err != nil {
			t.Fatalf("failed to render manifest: %v", err)
		}
		applyResourcesIntoCluster(t, h, manifestMap)
		if err := h.DeleteControlPlaneByManifests(manifestMap, testRevision, false); err != nil {
			t.Fatalf("HelmReconciler.DeleteControlPlaneByManifests() error = %v", err)
		}
		for _, gvk := range append(NamespacedResources, ClusterCPResources...) {
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
			if err := h.ApplyObject(obj.UnstructuredObject()); err != nil {
				t.Errorf("HelmReconciler.ApplyObject() error = %v", err)
			}
		}
	}
}
