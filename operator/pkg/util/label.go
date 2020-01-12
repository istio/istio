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

package util

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

var (
	// resourceAccessor is a common resource accessor
	accessor = meta.NewAccessor()
)

// HasLabel is a helper function returning true if the specified object contains the specified label.
func HasLabel(resource runtime.Object, label string) bool {
	labels, err := accessor.Labels(resource)
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

	if HasLabel(resource, label) {
		labels, _ := accessor.Labels(resource)
		delete(labels, label)
		resourceAccessor.SetLabels(labels)
	}
}

// GetLabel is a helper function which returns the value of the specified label on the specified object.
// returns "", false if the label was not found on the object.
func GetLabel(resource runtime.Object, label string) (string, bool) {
	if HasLabel(resource, label) {
		labels, _ := accessor.Labels(resource)
		value, ok := labels[label]
		return value, ok
	}
	return "", false
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
