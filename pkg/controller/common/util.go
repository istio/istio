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

package common

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

// IndexOf is a helper function returning the index of the specified string in the specified slice or -1 if not found.
func IndexOf(l []string, s string) int {
	for i, elem := range l {
		if elem == s {
			return i
		}
	}
	return -1
}

// HasLabel is a helper function returning true if the specified object contains the specified label.
func HasLabel(resource runtime.Object, label string) bool {
	labels, err := meta.NewAccessor().Labels(resource)
	if err != nil {
		// if we can't access labels, then it doesn't have this label
		return false
	}
	if labels == nil {
		return false
	}
	_, ok := labels[label]
	return ok
}

// DeleteLabel is a helper function which deletes the specified label from the specified object.
func DeleteLabel(resource runtime.Object, label string) {
	resourceAccessor, err := meta.Accessor(resource)
	if err != nil {
		// if we can't access labels, then it doesn't have this label, so nothing to delete
		return
	}
	labels := resourceAccessor.GetLabels()
	if labels == nil {
		return
	}
	delete(labels, label)
	resourceAccessor.SetLabels(labels)
}

// SetLabel is a helper function which sets the specified label and value on the specified object.
func SetLabel(resource runtime.Object, label, value string) error {
	resourceAccessor, err := meta.Accessor(resource)
	if err != nil {
		return err
	}
	labels := resourceAccessor.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[label] = value
	resourceAccessor.SetLabels(labels)
	return nil
}

// HasAnnotation is a helper function returning true if the specified object contains the specified annotation.
func HasAnnotation(resource runtime.Object, annotation string) bool {
	annotations, err := meta.NewAccessor().Annotations(resource)
	if err != nil {
		// if we can't access annotations, then it doesn't have this annotation
		return false
	}
	if annotations == nil {
		return false
	}
	_, ok := annotations[annotation]
	return ok
}

// DeleteAnnotation is a helper function which deletes the specified annotation from the specified object.
func DeleteAnnotation(resource runtime.Object, annotation string) {
	resourceAccessor, err := meta.Accessor(resource)
	if err != nil {
		// if we can't access annotations, then it doesn't have this annotation, so nothing to delete
		return
	}
	annotations := resourceAccessor.GetAnnotations()
	if annotations == nil {
		return
	}
	delete(annotations, annotation)
	resourceAccessor.SetAnnotations(annotations)
}

// GetAnnotation is a helper function which returns the value of the specified annotation on the specified object.
// returns ok=false if the annotation was not found on the object.
func GetAnnotation(resource runtime.Object, annotation string) (value string, ok bool) {
	annotations, err := meta.NewAccessor().Annotations(resource)
	if err != nil {
		// if we can't access annotations, then it doesn't have this annotation
		return
	}
	if annotations == nil {
		annotations = map[string]string{}
	}
	value, ok = annotations[annotation]
	return
}

// SetAnnotation is a helper function which sets the specified annotation and value on the specified object.
func SetAnnotation(resource runtime.Object, annotation, value string) error {
	resourceAccessor, err := meta.Accessor(resource)
	if err != nil {
		return err
	}
	annotations := resourceAccessor.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[annotation] = value
	resourceAccessor.SetAnnotations(annotations)
	return nil
}
