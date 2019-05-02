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

package admission

import (
	"net/http"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/patch"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
)

// ErrorResponse creates a new Response for error-handling a request.
func ErrorResponse(code int32, err error) types.Response {
	return types.Response{
		Response: &admissionv1beta1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Code:    code,
				Message: err.Error(),
			},
		},
	}
}

// ValidationResponse returns a response for admitting a request.
func ValidationResponse(allowed bool, reason string) types.Response {
	resp := types.Response{
		Response: &admissionv1beta1.AdmissionResponse{
			Allowed: allowed,
		},
	}
	if len(reason) > 0 {
		resp.Response.Result = &metav1.Status{
			Reason: metav1.StatusReason(reason),
		}
	}
	return resp
}

// PatchResponse returns a new response with json patch.
func PatchResponse(original, current runtime.Object) types.Response {
	patches, err := patch.NewJSONPatch(original, current)
	if err != nil {
		return ErrorResponse(http.StatusInternalServerError, err)
	}
	return types.Response{
		Patches: patches,
		Response: &admissionv1beta1.AdmissionResponse{
			Allowed:   true,
			PatchType: func() *admissionv1beta1.PatchType { pt := admissionv1beta1.PatchTypeJSONPatch; return &pt }(),
		},
	}
}
