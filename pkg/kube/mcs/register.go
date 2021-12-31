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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	mcs "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/istio/pilot/pkg/features"
)

var (
	schemeBuilder = &runtime.SchemeBuilder{}

	// AddToScheme is used to register MCS CRDs to a runtime.Scheme
	AddToScheme = schemeBuilder.AddToScheme

	// MCSSchemeGroupVersion is group version used to register Kubernetes Multi-Cluster Services (MCS) objects
	MCSSchemeGroupVersion = schema.GroupVersion{Group: features.MCSAPIGroup, Version: features.MCSAPIVersion}

	ServiceExportGVR = MCSSchemeGroupVersion.WithResource("serviceexports")
	ServiceImportGVR = MCSSchemeGroupVersion.WithResource("serviceimports")
)

func init() {
	schemeBuilder.Register(addKnownTypes)
}

func addKnownTypes(scheme *runtime.Scheme) error {
	// Register Kubernetes Multi-Cluster Services (MCS) objects.
	scheme.AddKnownTypes(MCSSchemeGroupVersion,
		&mcs.ServiceExport{},
		&mcs.ServiceExportList{},
		&mcs.ServiceImport{},
		&mcs.ServiceImportList{})
	metav1.AddToGroupVersion(scheme, MCSSchemeGroupVersion)

	return nil
}
