/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package admission

import (
	"fmt"

	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	scheme = runtime.NewScheme()
	codecs = serializer.NewCodecFactory(scheme)
)

// Decode reads the Raw data from review and deserializes it into object returning a non-nil response if there was an
// error
func Decode(review v1beta1.AdmissionReview, object runtime.Object,
	resourceType metav1.GroupVersionResource) *v1beta1.AdmissionResponse {
	if review.Request.Resource != resourceType {
		return ErrorResponse(fmt.Errorf("expect resource to be %s", resourceType))
	}

	raw := review.Request.Object.Raw
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, object); err != nil {
		fmt.Printf("%v", err)
		return ErrorResponse(err)
	}
	return nil
}
