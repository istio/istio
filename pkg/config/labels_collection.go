// Copyright 2017 Istio Authors
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

// This file describes the abstract model of services (and their instances) as
// represented in Istio. This model is independent of the underlying platform
// (Kubernetes, Mesos, etc.). Platform specific adapters found populate the
// model object with various fields, from the metadata found in the platform.
// The platform independent proxy code uses the representation in the model to
// generate the configuration files for the Layer 7 proxy sidecar. The proxy
// code is specific to individual proxy implementations

package config

// LabelsCollection is a collection of labels used for comparing labels against a
// collection of labels
type LabelsCollection []Labels

// HasSubsetOf returns true if the input labels are a super set of one labels in a
// collection or if the tag collection is empty
func (labels LabelsCollection) HasSubsetOf(that Labels) bool {
	if len(labels) == 0 {
		return true
	}
	for _, label := range labels {
		if label.SubsetOf(that) {
			return true
		}
	}
	return false
}

// IsSupersetOf returns true if the input labels are a subset set of any set of labels in a
// collection
func (labels LabelsCollection) IsSupersetOf(that Labels) bool {

	if len(labels) == 0 {
		return len(that) == 0
	}

	for _, label := range labels {
		if that.SubsetOf(label) {
			return true
		}
	}
	return false
}
