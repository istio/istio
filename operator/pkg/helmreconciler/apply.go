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
	"istio.io/istio/operator/pkg/util"
)

const fieldOwnerOperator = "istio-operator"

// AppliedResult is the result of applying a Manifest.
type AppliedResult struct {
	// processedObjects is the list of objects that were processed in this apply operation.
	processedObjects object.K8sObjects
	// deployed is the number of objects have been deployed which means
	// it's in the cache and it's not changed from the cache.
	deployed int
}

// Succeed returns true if the apply operation succeeded.
func (r AppliedResult) Succeed() bool {
	return len(r.processedObjects) > 0 || r.deployed > 0
}

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

	// Objects are applied in groups: namespaces, CRDs, everything else, with wait for ready in between.
	nsObjs := object.KindObjects(allObjects, name.NamespaceStr)
	crdObjs := object.KindObjects(allObjects, name.CRDStr)
	otherObjs := object.ObjectsNotInLists(allObjects, nsObjs, crdObjs)
	var processedObjects object.K8sObjects
	for _, objList := range []object.K8sObjects{nsObjs, crdObjs, otherObjs} {
		// For a given group of objects, apply in sorted order of priority with no wait in between.
		objList.Sort(object.DefaultObjectOrder())
		for _, obj := range objList {
			obju := obj.UnstructuredObject()
			if err := h.applyLabelsAndAnnotations(obju, cname); err != nil {
				return err
			}
			if err := h.ApplyObject(obj.UnstructuredObject()); err != nil {
				plog.ReportError(err.Error())
				return err
			}
			plog.ReportProgress()
			processedObjects = append(processedObjects, obj)
		}
	}

	if err := WaitForResources(processedObjects, h.kubeClient, h.opts.WaitTimeout, h.opts.DryRun, plog); err != nil {
		werr := fmt.Errorf("failed to wait for resource: %v", err)
		plog.ReportError(werr.Error())
		return werr
	}
	plog.ReportFinished()
	return nil
}

// ApplyObject creates or updates an object in the API server depending on whether it already exists.
// It mutates obj.
func (h *HelmReconciler) ApplyObject(obj *unstructured.Unstructured) error {
	if obj.GetKind() == "List" {
		var errs util.Errors
		list, err := obj.ToList()
		if err != nil {
			scope.Errorf("error converting List object: %s", err)
			return err
		}
		for _, item := range list.Items {
			err = h.ApplyObject(&item)
			if err != nil {
				errs = util.AppendErr(errs, err)
			}
		}
		return errs.ToError()
	}

	objectStr := fmt.Sprintf("%s/%s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName())

	if scope.DebugEnabled() {
		scope.Debugf("Processing object:\n%s\n\n", util.ToYAML(obj))
	}

	if h.opts.DryRun {
		scope.Infof("Not applying object %s because of dry run.", objectStr)
		return nil
	}

	return h.serverSideApply(obj)
}

// use server-side apply, require kubernetes 1.16+
func (h *HelmReconciler) serverSideApply(obj *unstructured.Unstructured) error {
	objectStr := fmt.Sprintf("%s/%s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName())
	scope.Infof("using server side apply to update obj: %v", objectStr)
	opts := []client.PatchOption{client.ForceOwnership, client.FieldOwner(fieldOwnerOperator)}
	if err := h.client.Patch(context.TODO(), obj, client.Apply, opts...); err != nil {
		return fmt.Errorf("failed to update resource with server-side apply for obj %v: %v", objectStr, err)
	}
	return nil
}
