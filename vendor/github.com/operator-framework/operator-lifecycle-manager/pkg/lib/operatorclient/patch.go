package operatorclient

import (
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	v1beta1ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

// UpdateFunction defines a function that updates an object in an Update* function. The function
// provides the current instance of the object retrieved from the apiserver. The function should
// return the updated object to be applied.
type UpdateFunction func(current metav1.Object) (metav1.Object, error)

// Update returns a default UpdateFunction implementation that passes its argument through to the
// Update* function directly, ignoring the current object.
//
// Example usage:
//
// client.UpdateDaemonSet(namespace, name, types.Update(obj))
func Update(obj metav1.Object) UpdateFunction {
	return func(_ metav1.Object) (metav1.Object, error) {
		return obj, nil
	}
}

// PatchFunction defines a function that is used to provide patch objects for a 3-way merge. The
// function provides the current instance of the object retrieved from the apiserver. The function
// should return the "original" and "modified" objects (in that order) for 3-way patch computation.
type PatchFunction func(current metav1.Object) (metav1.Object, metav1.Object, error)

// Patch returns a default PatchFunction implementation that passes its arguments through to the
// patcher directly, ignoring the current object.
//
// Example usage:
//
// client.PatchDaemonSet(namespace, name, types.Patch(original, current))
func Patch(original metav1.Object, modified metav1.Object) PatchFunction {
	return func(_ metav1.Object) (metav1.Object, metav1.Object, error) {
		return original, modified, nil
	}
}

// updateToPatch wraps an UpdateFunction as a PatchFunction.
func updateToPatch(f UpdateFunction) PatchFunction {
	return func(obj metav1.Object) (metav1.Object, metav1.Object, error) {
		obj, err := f(obj)
		return nil, obj, err
	}
}

func createPatch(original, modified runtime.Object) ([]byte, error) {
	originalData, err := json.Marshal(original)
	if err != nil {
		return nil, err
	}
	modifiedData, err := json.Marshal(modified)
	if err != nil {
		return nil, err
	}
	return strategicpatch.CreateTwoWayMergePatch(originalData, modifiedData, original)
}

func createThreeWayMergePatchPreservingCommands(original, modified, current runtime.Object) ([]byte, error) {
	var datastruct runtime.Object
	switch {
	case original != nil:
		datastruct = original
	case modified != nil:
		datastruct = modified
	case current != nil:
		datastruct = current
	default:
		return nil, nil // A 3-way merge of `nil`s is `nil`.
	}
	patchMeta, err := strategicpatch.NewPatchMetaFromStruct(datastruct)
	if err != nil {
		return nil, err
	}

	// Create normalized clones of objects.
	original, err = cloneAndNormalizeObject(original)
	if err != nil {
		return nil, err
	}
	modified, err = cloneAndNormalizeObject(modified)
	if err != nil {
		return nil, err
	}
	current, err = cloneAndNormalizeObject(current)
	if err != nil {
		return nil, err
	}
	// Perform 3-way merge of annotations and labels.
	if err := mergeAnnotationsAndLabels(original, modified, current); err != nil {
		return nil, err
	}
	// Construct 3-way JSON merge patch.
	originalData, err := json.Marshal(original)
	if err != nil {
		return nil, err
	}
	modifiedData, err := json.Marshal(modified)
	if err != nil {
		return nil, err
	}
	currentData, err := json.Marshal(current)
	if err != nil {
		return nil, err
	}
	return strategicpatch.CreateThreeWayMergePatch(originalData, modifiedData, currentData, patchMeta, false /* overwrite */)
}

func cloneAndNormalizeObject(obj runtime.Object) (runtime.Object, error) {
	if obj == nil {
		return obj, nil
	}

	// Clone the object since it will be modified.
	obj = obj.DeepCopyObject()
	switch obj := obj.(type) {
	case *appsv1.DaemonSet:
		if obj != nil {
			// These are only extracted from current; should not be considered for diffs.
			obj.ObjectMeta.ResourceVersion = ""
			obj.ObjectMeta.CreationTimestamp = metav1.Time{}
			obj.Status = appsv1.DaemonSetStatus{}
		}
	case *appsv1.Deployment:
		if obj != nil {
			// These are only extracted from current; should not be considered for diffs.
			obj.ObjectMeta.ResourceVersion = ""
			obj.ObjectMeta.CreationTimestamp = metav1.Time{}
			obj.Status = appsv1.DeploymentStatus{}
		}
	case *v1.Service:
		if obj != nil {
			// These are only extracted from current; should not be considered for diffs.
			obj.ObjectMeta.ResourceVersion = ""
			obj.ObjectMeta.CreationTimestamp = metav1.Time{}
			obj.Status = v1.ServiceStatus{}
			// ClusterIP for service is immutable, so cannot patch.
			obj.Spec.ClusterIP = ""
		}
	case *extensionsv1beta1.Ingress:
		if obj != nil {
			// These are only extracted from current; should not be considered for diffs.
			obj.ObjectMeta.ResourceVersion = ""
			obj.ObjectMeta.CreationTimestamp = metav1.Time{}
			obj.Status = extensionsv1beta1.IngressStatus{}
		}
	case *v1beta1ext.CustomResourceDefinition:
		if obj != nil {
			// These are only extracted from current; should not be considered for diffs.
			obj.ObjectMeta.ResourceVersion = ""
			obj.ObjectMeta.CreationTimestamp = metav1.Time{}
			obj.ObjectMeta.SelfLink = ""
			obj.ObjectMeta.UID = ""
			obj.Status = v1beta1ext.CustomResourceDefinitionStatus{}
		}
	default:
		return nil, fmt.Errorf("unhandled type: %T", obj)
	}
	return obj, nil
}

// mergeAnnotationsAndLabels performs a 3-way merge of all annotations and labels using custom
// 3-way merge logic defined in mergeMaps() below.
func mergeAnnotationsAndLabels(original, modified, current runtime.Object) error {
	if original == nil || modified == nil || current == nil {
		return nil
	}

	accessor := meta.NewAccessor()
	if err := mergeMaps(original, modified, current, accessor.Annotations, accessor.SetAnnotations); err != nil {
		return err
	}
	if err := mergeMaps(original, modified, current, accessor.Labels, accessor.SetLabels); err != nil {
		return err
	}

	switch current := current.(type) {
	case *appsv1.DaemonSet:
		getter := func(obj runtime.Object) (map[string]string, error) {
			return obj.(*appsv1.DaemonSet).Spec.Template.Annotations, nil
		}
		setter := func(obj runtime.Object, val map[string]string) error {
			obj.(*appsv1.DaemonSet).Spec.Template.Annotations = val
			return nil
		}
		if err := mergeMaps(original, modified, current, getter, setter); err != nil {
			return err
		}
		getter = func(obj runtime.Object) (map[string]string, error) {
			return obj.(*appsv1.DaemonSet).Spec.Template.Labels, nil
		}
		setter = func(obj runtime.Object, val map[string]string) error {
			obj.(*appsv1.DaemonSet).Spec.Template.Labels = val
			return nil
		}
		if err := mergeMaps(original, modified, current, getter, setter); err != nil {
			return err
		}
	case *appsv1.Deployment:
		getter := func(obj runtime.Object) (map[string]string, error) {
			return obj.(*appsv1.Deployment).Spec.Template.Annotations, nil
		}
		setter := func(obj runtime.Object, val map[string]string) error {
			obj.(*appsv1.Deployment).Spec.Template.Annotations = val
			return nil
		}
		if err := mergeMaps(original, modified, current, getter, setter); err != nil {
			return err
		}
		getter = func(obj runtime.Object) (map[string]string, error) {
			return obj.(*appsv1.Deployment).Spec.Template.Labels, nil
		}
		setter = func(obj runtime.Object, val map[string]string) error {
			obj.(*appsv1.Deployment).Spec.Template.Labels = val
			return nil
		}
		if err := mergeMaps(original, modified, current, getter, setter); err != nil {
			return err
		}
	}
	return nil
}

// mergeMaps creates a patch using createThreeWayMapPatch and if the patch is non-empty applies
// the patch to the input. The getter and setter are used to access the map inside the given
// objects.
func mergeMaps(original, modified, current runtime.Object, getter func(runtime.Object) (map[string]string, error), setter func(runtime.Object, map[string]string) error) error {
	originalMap, err := getter(original)
	if err != nil {
		return err
	}
	modifiedMap, err := getter(modified)
	if err != nil {
		return err
	}
	currentMap, err := getter(current)
	if err != nil {
		return err
	}

	patch, err := createThreeWayMapPatch(originalMap, modifiedMap, currentMap)
	if err != nil {
		return err
	}
	if len(patch) == 0 {
		return nil // nothing to apply.
	}
	modifiedMap = applyMapPatch(originalMap, currentMap, patch)

	if err := setter(original, originalMap); err != nil {
		return err
	}
	if err := setter(modified, modifiedMap); err != nil {
		return err
	}
	return setter(current, currentMap)
}

// applyMapPatch creates a copy of current and applies the three-way map patch to it.
func applyMapPatch(original, current map[string]string, patch map[string]interface{}) map[string]string {
	merged := make(map[string]string, len(current))
	for k, v := range current {
		merged[k] = v
	}
	for k, v := range patch {
		if v == nil {
			delete(merged, k)
		} else {
			merged[k] = v.(string)
			if _, ok := current[k]; !ok {
				// If we are re-adding something that may have already been in original then ensure it is
				// removed from `original` to avoid a conflict in upstream patch code.
				delete(original, k)
			}
		}
	}
	return merged
}

// createThreeWayMapPatch constructs a 3-way patch between original, modified, and current. The
// patch contains only keys that are added, keys that are removed (with their values set to nil) or
// keys whose values are modified. Returns an error if there is a conflict for any key.
//
// The behavior is defined as follows:
//
// - If an item is present in modified, ensure it exists in current.
// - If an item is present in original and removed in modified, remove it from current.
// - If an item is present only in current, leave it as-is.
//
// This effectively "enforces" that all items present in modified are present in current, and all
// items deleted from original => modified are deleted in current.
//
// The following will cause a conflict:
//
// (1) An item was deleted from original => modified but modified from original => current.
// (2) An item was modified differently from original => modified and original => current.
func createThreeWayMapPatch(original, modified, current map[string]string) (map[string]interface{}, error) {
	// Create union of keys.
	keys := make(map[string]struct{})
	for k := range original {
		keys[k] = struct{}{}
	}
	for k := range modified {
		keys[k] = struct{}{}
	}
	for k := range current {
		keys[k] = struct{}{}
	}

	// Create patch according to rules.
	patch := make(map[string]interface{})
	for k := range keys {
		oVal, oOk := original[k]
		mVal, mOk := modified[k]
		cVal, cOk := current[k]

		switch {
		case oOk && mOk && cOk:
			// present in all three.
			if mVal != cVal {
				if oVal != cVal {
					// conflict type 2: changed to different values in modified and current.
					return nil, fmt.Errorf("conflict at key %v: original = %v, modified = %v, current = %v", k, oVal, mVal, cVal)
				}
				patch[k] = mVal
			}
		case !oOk && mOk && cOk:
			// added in modified and current.
			if mVal != cVal {
				// conflict type 2: added different values in modified and current.
				return nil, fmt.Errorf("conflict at key %v: original = <absent>, modified = %v, current = %v", k, mVal, cVal)
			}
		case oOk && !mOk && cOk:
			// deleted in modified.
			if oVal != cVal {
				// conflict type 1: changed from original to current, removed in modified.
				return nil, fmt.Errorf("conflict at key %v, original = %v, modified = <absent>, current = %v", k, oVal, cVal)
			}
			patch[k] = nil
		case oOk && mOk && !cOk:
			// deleted in current.
			patch[k] = mVal
		case !oOk && !mOk && cOk:
			// only exists in current.
		case !oOk && mOk && !cOk:
			// added in modified.
			patch[k] = mVal
		case oOk && !mOk && !cOk:
			// deleted in both modified and current.
		case !oOk && !mOk && !cOk:
			// unreachable.
		}
	}
	return patch, nil
}
