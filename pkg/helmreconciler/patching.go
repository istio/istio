// Copyright 2019 Istio Authors
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

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/mergepatch"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes/scheme"
	kubectl "k8s.io/kubectl/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreatePatch creates a patch based on the current and updated versions of an object
func (h *HelmReconciler) CreatePatch(current, updated runtime.Object) (Patch, error) {
	var retVal Patch
	patch := &basicPatch{client: h.client}
	currentAccessor, err := meta.Accessor(current)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("cannot create object accessor for current object:\n%v", current))
	} else if updatedAccessor, err := meta.Accessor(updated); err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("cannot create object accessor for updated object:\n%v", updated))
	} else {
		updatedAccessor.SetResourceVersion(currentAccessor.GetResourceVersion())
	}

	// Serialize the current configuration of the object.
	patch.currentBytes, err = runtime.Encode(unstructured.UnstructuredJSONScheme, current)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("could not serialize current object into raw json:\n%v", current))
	}

	// Serialize the updated configuration of the object.
	updatedBytes, err := runtime.Encode(unstructured.UnstructuredJSONScheme, updated)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("could not serialize updated object into raw json:\n%v", updated))
	}

	// Retrieve the original configuration of the object.
	originalBytes, err := kubectl.GetOriginalConfiguration(current)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("failed to retrieve original configuration from current object:\n%v", current))
	}

	var gvk schema.GroupVersionKind
	if currentTypeAccessor, err := meta.TypeAccessor(current); err == nil {
		gvk = schema.FromAPIVersionAndKind(currentTypeAccessor.GetAPIVersion(), currentTypeAccessor.GetKind())
	} else {
		return nil, errors.Wrap(err, fmt.Sprintf("error getting GroupVersionKind for object"))
	}

	// if we can get a versioned object from the scheme, we can use the strategic patching mechanism
	// (i.e. take advantage of patchStrategy in the type)
	versionedObject, err := scheme.Scheme.New(gvk)
	if err != nil {
		// json merge patch
		preconditions := []mergepatch.PreconditionFunc{
			mergepatch.RequireKeyUnchanged("apiVersion"),
			mergepatch.RequireKeyUnchanged("kind"),
			mergepatch.RequireMetadataKeyUnchanged("name"),
			mergepatch.RequireMetadataKeyUnchanged("namespace"),
		}
		patch.patchBytes, err = jsonmergepatch.CreateThreeWayJSONMergePatch(originalBytes, updatedBytes, patch.currentBytes, preconditions...)
		if err != nil {
			if mergepatch.IsPreconditionFailed(err) {
				return nil, errors.Wrap(err, fmt.Sprintf("cannot change apiVersion, kind, name, or namespace fields"))
			}
			return nil, errors.Wrap(err,
				fmt.Sprintf(
					"could not create patch for object, original:\n%v\ncurrent:\n%v\nupdated:\n%v",
					originalBytes, patch.currentBytes, updatedBytes))
		}
		retVal = &jsonMergePatch{basicPatch: patch}
	} else {
		// XXX: if we fail to create a strategic patch, should we fall back to json merge patch?
		// strategic merge patch
		lookupPatchMeta, err := strategicpatch.NewPatchMetaFromStruct(versionedObject)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("could not retrieve patch metadata for object: %s", gvk.String()))
		}
		patch.patchBytes, err = strategicpatch.CreateThreeWayMergePatch(originalBytes, updatedBytes, patch.currentBytes, lookupPatchMeta, true)
		if err != nil {
			return nil, errors.Wrap(err,
				fmt.Sprintf(
					"could not create patch for object, original:\n%v\ncurrent:\n%v\nupdated:\n%v",
					originalBytes, patch.currentBytes, updatedBytes))
		}
		retVal = &strategicMergePatch{basicPatch: patch, schema: lookupPatchMeta}
	}

	if string(patch.patchBytes) == "{}" {
		// empty patch, nothing to do
		return nil, nil
	}

	return retVal, nil
}

type basicPatch struct {
	client       client.Client
	patchBytes   []byte
	currentBytes []byte
}
type strategicMergePatch struct {
	*basicPatch
	schema strategicpatch.LookupPatchMeta
}

var _ Patch = &strategicMergePatch{}

func (p *strategicMergePatch) Apply() (*unstructured.Unstructured, error) {
	newBytes, err := strategicpatch.StrategicMergePatchUsingLookupPatchMeta(p.currentBytes, p.patchBytes, p.schema)
	if err != nil {
		return nil, err
	}
	newObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, newBytes)
	if err != nil {
		return nil, err
	}
	if err = p.client.Update(context.TODO(), newObj); err != nil {
		return nil, err
	}
	if newUnstructured, ok := newObj.(*unstructured.Unstructured); ok {
		return newUnstructured, nil
	}
	return nil, fmt.Errorf("could not decode unstructured object:\n%v", newObj)
}

type jsonMergePatch struct {
	*basicPatch
}

var _ Patch = &jsonMergePatch{}

func (p *jsonMergePatch) Apply() (*unstructured.Unstructured, error) {
	newBytes, err := jsonpatch.MergePatch(p.currentBytes, p.patchBytes)
	if err != nil {
		return nil, err
	}
	newObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, newBytes)
	if err != nil {
		return nil, err
	}
	if err = p.client.Update(context.TODO(), newObj); err != nil {
		return nil, err
	}
	if newUnstructured, ok := newObj.(*unstructured.Unstructured); ok {
		return newUnstructured, nil
	}
	return nil, fmt.Errorf("could not decode unstructured object:\n%v", newObj)
}
