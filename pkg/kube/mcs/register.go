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

package mcs

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	mcs "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
)

var (
	ServiceExportGVR = schema.GroupVersionResource{
		Group:    mcs.GroupName,
		Version:  mcs.GroupVersion.Version,
		Resource: "serviceexports",
	}
	ServiceImportGVR = schema.GroupVersionResource{
		Group:    mcs.GroupName,
		Version:  mcs.GroupVersion.Version,
		Resource: "serviceimports",
	}
)
