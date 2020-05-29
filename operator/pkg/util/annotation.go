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
	"k8s.io/apimachinery/pkg/runtime"
)

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
