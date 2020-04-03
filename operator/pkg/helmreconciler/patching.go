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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
