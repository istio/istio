/*
Copyright 2018 The Kubernetes Authors.

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

package types

import (
	"github.com/appscode/jsonpatch"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Request is the input of Handler
type Request struct {
	AdmissionRequest *admissionv1beta1.AdmissionRequest
}

// Response is the output of admission.Handler
type Response struct {
	// Patches are the JSON patches for mutating webhooks.
	// Using this instead of setting Response.Patch to minimize the overhead of serialization and deserialization.
	Patches []jsonpatch.JsonPatchOperation
	// Response is the admission response. Don't set the Patch field in it.
	Response *admissionv1beta1.AdmissionResponse
}

// Decoder is used to decode AdmissionRequest.
type Decoder interface {
	// Decode decodes the raw byte object from the AdmissionRequest to the passed-in runtime.Object.
	Decode(Request, runtime.Object) error
}
