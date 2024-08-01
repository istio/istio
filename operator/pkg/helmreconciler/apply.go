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
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
)

const fieldOwnerOperator = "istio-operator"

// ApplyManifest applies the manifest to create or update resources. It returns the processed (created or updated)
// objects and the number of objects in the manifests.
func (h *HelmReconciler) ApplyManifest(manifest name.Manifest) error {
	cname := string(manifest.Name)

	scope.Infof("Processing resources from manifest: %s", cname)
	allObjects, err := object.ParseK8sObjectsFromYAMLManifest(manifest.Content)
	if err != nil {
		return err
	}

	plog := h.opts.ProgressLog.NewComponent(cname)

	allObjects.Sort(object.DefaultObjectOrder())
	for _, obj := range allObjects {
		obju := obj.UnstructuredObject()
		if err := h.applyLabelsAndAnnotations(obju, cname); err != nil {
			return err
		}
		if err := h.ServerSideApply(obj.UnstructuredObject()); err != nil {
			plog.ReportError(err.Error())
			return err
		}
		plog.ReportProgress()
	}

	if err := WaitForResources(allObjects, h.kubeClient, h.opts.WaitTimeout, h.opts.DryRun, plog); err != nil {
		werr := fmt.Errorf("failed to wait for resource: %v", err)
		plog.ReportError(werr.Error())
		return werr
	}
	plog.ReportFinished()
	return nil
}

// ServerSideApply creates or updates an object in the API server depending on whether it already exists.
func (h *HelmReconciler) ServerSideApply(obj *unstructured.Unstructured) error {
	objectStr := fmt.Sprintf("%s/%s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName())
	scope.Infof("using server side apply to update obj: %v", objectStr)
	opts := []client.PatchOption{client.ForceOwnership, client.FieldOwner(fieldOwnerOperator)}
	if err := h.client.Patch(context.TODO(), obj, client.Apply, opts...); err != nil {
		return fmt.Errorf("failed to update resource with server-side apply for obj %v: %v", objectStr, err)
	}
	return nil
}
