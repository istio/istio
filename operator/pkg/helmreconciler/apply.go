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
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/retry"
	kubectlutil "k8s.io/kubectl/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/metrics"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/progress"
)

const fieldOwnerOperator = "istio-operator"

// ApplyManifest applies the manifest to create or update resources. It returns the processed (created or updated)
// objects and the number of objects in the manifests.
func (h *HelmReconciler) ApplyManifest(manifest name.Manifest, serverSideApply bool) (object.K8sObjects, int, error) {
	var processedObjects object.K8sObjects
	var deployedObjects int
	var errs util.Errors
	cname := string(manifest.Name)
	crHash, err := h.getCRHash(cname)
	if err != nil {
		return nil, 0, err
	}

	scope.Infof("Processing resources from manifest: %s for CR %s", cname, crHash)
	allObjects, err := object.ParseK8sObjectsFromYAMLManifest(manifest.Content)
	if err != nil {
		return nil, 0, err
	}

	objectCache := cache.GetCache(crHash)

	// Ensure that for a given CR crHash only one control loop uses the per-crHash cache at any time.
	objectCache.Mu.Lock()
	defer objectCache.Mu.Unlock()

	// No further locking required beyond this point, since we have a ptr to a cache corresponding to a CR crHash and no
	// other controller is allowed to work on at the same time.
	var changedObjects object.K8sObjects
	var changedObjectKeys []string
	allObjectsMap := make(map[string]bool)

	// Check which objects in the manifest have changed from those in the cache.
	for _, obj := range allObjects {
		oh := obj.Hash()
		allObjectsMap[oh] = true
		if co, ok := objectCache.Cache[oh]; ok && obj.Equal(co) {
			// Object is in the cache and unchanged.
			metrics.AddResource(obj.FullName(), obj.GroupVersionKind().GroupKind())
			deployedObjects++
			continue
		}
		changedObjects = append(changedObjects, obj)
		changedObjectKeys = append(changedObjectKeys, oh)
	}

	var plog *progress.ManifestLog
	if len(changedObjectKeys) > 0 {
		plog = h.opts.ProgressLog.NewComponent(cname)
		scope.Infof("The following objects differ between generated manifest and cache: \n - %s", strings.Join(changedObjectKeys, "\n - "))
	} else {
		scope.Infof("Generated manifest objects are the same as cached for component %s.", cname)
	}

	// Objects are applied in groups: namespaces, CRDs, everything else, with wait for ready in between.
	nsObjs := object.KindObjects(changedObjects, name.NamespaceStr)
	crdObjs := object.KindObjects(changedObjects, name.CRDStr)
	otherObjs := object.ObjectsNotInLists(changedObjects, nsObjs, crdObjs)
	for _, objList := range []object.K8sObjects{nsObjs, crdObjs, otherObjs} {
		// For a given group of objects, apply in sorted order of priority with no wait in between.
		objList.Sort(object.DefaultObjectOrder())
		for _, obj := range objList {
			obju := obj.UnstructuredObject()
			if err := h.applyLabelsAndAnnotations(obju, cname); err != nil {
				return nil, 0, err
			}
			if err := h.ApplyObject(obj.UnstructuredObject(), serverSideApply); err != nil {
				scope.Error(err.Error())
				errs = util.AppendErr(errs, err)
				continue
			}
			plog.ReportProgress()
			metrics.AddResource(obj.FullName(), obj.GroupVersionKind().GroupKind())
			processedObjects = append(processedObjects, obj)
			// Update the cache with the latest object.
			objectCache.Cache[obj.Hash()] = obj
		}
	}

	// Prune anything not in the manifest out of the cache.
	var removeKeys []string
	for k := range objectCache.Cache {
		if !allObjectsMap[k] {
			removeKeys = append(removeKeys, k)
		}
	}
	for _, k := range removeKeys {
		scope.Infof("Pruning object %s from cache.", k)
		delete(objectCache.Cache, k)
	}

	if len(changedObjectKeys) > 0 {
		if len(errs) != 0 {
			plog.ReportError(util.ToString(errs.Dedup(), "\n"))
			return processedObjects, 0, errs.ToError()
		}

		err := WaitForResources(processedObjects, h.kubeClient,
			h.opts.WaitTimeout, h.opts.DryRun, plog)
		if err != nil {
			werr := fmt.Errorf("failed to wait for resource: %v", err)
			plog.ReportError(werr.Error())
			return processedObjects, 0, werr
		}
		plog.ReportFinished()

	}
	return processedObjects, deployedObjects, nil
}

// ApplyObject creates or updates an object in the API server depending on whether it already exists.
// It mutates obj.
func (h *HelmReconciler) ApplyObject(obj *unstructured.Unstructured, serverSideApply bool) error {
	if obj.GetKind() == "List" {
		var errs util.Errors
		list, err := obj.ToList()
		if err != nil {
			scope.Errorf("error converting List object: %s", err)
			return err
		}
		for _, item := range list.Items {
			err = h.ApplyObject(&item, serverSideApply)
			if err != nil {
				errs = util.AppendErr(errs, err)
			}
		}
		return errs.ToError()
	}

	if err := kubectlutil.CreateApplyAnnotation(obj, unstructured.UnstructuredJSONScheme); err != nil {
		scope.Errorf("unexpected error adding apply annotation to object: %s", err)
	}

	objectKey := client.ObjectKeyFromObject(obj)
	objectStr := fmt.Sprintf("%s/%s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName())

	if scope.DebugEnabled() {
		scope.Debugf("Processing object:\n%s\n\n", util.ToYAML(obj))
	}

	if h.opts.DryRun {
		scope.Infof("Not applying object %s because of dry run.", objectStr)
		return nil
	}

	if serverSideApply {
		return h.serverSideApply(obj)
	}

	receiver := &unstructured.Unstructured{}
	receiver.SetGroupVersionKind(obj.GroupVersionKind())
	gvk := obj.GetObjectKind().GroupVersionKind()
	// for k8s version before 1.16
	return retry.RetryOnConflict(conflictBackoff, func() error {
		err := h.client.Get(context.TODO(), objectKey, receiver)

		switch {
		case apierrors.IsNotFound(err):
			scope.Infof("Creating %s (%s/%s)", objectStr, h.iop.Name, h.iop.Spec.Revision)
			err = h.client.Create(context.TODO(), obj)
			if err != nil {
				return fmt.Errorf("failed to create %q: %w", objectStr, err)
			}
			metrics.ResourceCreationTotal.
				With(metrics.ResourceKindLabel.Value(util.GKString(gvk.GroupKind()))).
				Increment()
			return nil
		case err == nil:
			scope.Infof("Updating %s (%s/%s)", objectStr, h.iop.Name, h.iop.Spec.Revision)
			// The correct way to do this is with a server-side apply. However, this requires users to be running Kube 1.16.
			// When we no longer support < 1.16 use the code described in the linked issue.
			// https://github.com/kubernetes-sigs/controller-runtime/issues/347
			if err := applyOverlay(receiver, obj); err != nil {
				return err
			}
			if err := h.client.Update(context.TODO(), receiver); err != nil {
				return err
			}
			metrics.ResourceUpdateTotal.
				With(metrics.ResourceKindLabel.Value(util.GKString(gvk.GroupKind()))).
				Increment()
			return nil
		}
		return nil
	})
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
