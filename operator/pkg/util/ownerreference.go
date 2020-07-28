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

package util

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func referSameObject(a, b metav1.OwnerReference) bool {
	aGV, err := schema.ParseGroupVersion(a.APIVersion)
	if err != nil {
		return false
	}

	bGV, err := schema.ParseGroupVersion(b.APIVersion)
	if err != nil {
		return false
	}

	return aGV.Group == bGV.Group && a.Kind == b.Kind && a.Name == b.Name
}

func indexOwnerRef(ownerReferences []metav1.OwnerReference, ref metav1.OwnerReference) int {
	for index, r := range ownerReferences {
		if referSameObject(r, ref) {
			return index
		}
	}
	return -1
}

func upsertOwnerRef(ref metav1.OwnerReference, object metav1.Object) {
	owners := object.GetOwnerReferences()
	idx := indexOwnerRef(owners, ref)
	if idx == -1 {
		owners = append(owners, ref)
	} else {
		owners[idx] = ref
	}
	object.SetOwnerReferences(owners)
}

// SetLabel is a helper function which sets the specified label and value on te specified object.
func SetOwnerReference(resource runtime.Object, ownerRef metav1.OwnerReference) error {
	resourceAccessor, err := meta.Accessor(resource)
	if err != nil {
		return err
	}
	ownerRefs := resourceAccessor.GetOwnerReferences()
	if ownerRefs == nil {
		ownerRefs = []metav1.OwnerReference{}
	}
	ownerRefs = append(ownerRefs, ownerRef)
	resourceAccessor.SetOwnerReferences(ownerRefs)
	return nil
}
