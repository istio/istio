//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"fmt"

	kubeApiAdmissionv1 "k8s.io/api/admission/v1"
	kubeApiAdmissionv1beta1 "k8s.io/api/admission/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// APIVersion constants
	admissionAPIV1      = "admission.k8s.io/v1"
	admissionAPIV1beta1 = "admission.k8s.io/v1beta1"

	// Operation constants
	Create  string = "CREATE"
	Update  string = "UPDATE"
	Delete  string = "DELETE"
	Connect string = "CONNECT"
)

// AdmissionReview describes an admission review request/response.
type AdmissionReview struct {
	// TypeMeta describes an individual object in an API response or request
	// with strings representing the type of the object and its API schema version.
	// Structures that are versioned or persisted should inline TypeMeta.
	metav1.TypeMeta

	// Request describes the attributes for the admission request.
	Request *AdmissionRequest `json:"request,omitempty"`

	// Response describes the attributes for the admission response.
	Response *AdmissionResponse `json:"response,omitempty"`
}

// AdmissionRequest describes the admission.Attributes for the admission request.
type AdmissionRequest struct {

	// UID is an identifier for the individual request/response. It allows us to distinguish instances of requests which are
	// otherwise identical (parallel requests, requests when earlier requests did not modify etc)
	// The UID is meant to track the round trip (request/response) between the KAS and the WebHook, not the user request.
	// It is suitable for correlating log entries between the webhook and apiserver, for either auditing or debugging.
	UID types.UID `json:"uid"`

	// Kind is the fully-qualified type of object being submitted (for example, v1.Pod or autoscaling.v1.Scale)
	Kind metav1.GroupVersionKind `json:"kind"`

	// Resource is the fully-qualified resource being requested (for example, v1.pods)
	Resource metav1.GroupVersionResource `json:"resource"`

	// SubResource is the subresource being requested, if any (for example, "status" or "scale")
	SubResource string `json:"subResource,omitempty"`
	// RequestKind is the fully-qualified type of the original API request (for example, v1.Pod or autoscaling.v1.Scale).
	// If this is specified and differs from the value in "kind", an equivalent match and conversion was performed.
	//
	// For example, if deployments can be modified via apps/v1 and apps/v1beta1, and a webhook registered a rule of
	// `apiGroups:["apps"], apiVersions:["v1"], resources: ["deployments"]` and `matchPolicy: Equivalent`,
	// an API request to apps/v1beta1 deployments would be converted and sent to the webhook
	// with `kind: {group:"apps", version:"v1", kind:"Deployment"}` (matching the rule the webhook registered for),
	// and `requestKind: {group:"apps", version:"v1beta1", kind:"Deployment"}` (indicating the kind of the original API request).
	//
	RequestKind *metav1.GroupVersionKind `json:"requestKind,omitempty"`

	// RequestResource is the fully-qualified resource of the original API request (for example, v1.pods).
	// If this is specified and differs from the value in "resource", an equivalent match and conversion was performed.
	//
	// For example, if deployments can be modified via apps/v1 and apps/v1beta1, and a webhook registered a rule of
	// `apiGroups:["apps"], apiVersions:["v1"], resources: ["deployments"]` and `matchPolicy: Equivalent`,
	// an API request to apps/v1beta1 deployments would be converted and sent to the webhook
	// with `resource: {group:"apps", version:"v1", resource:"deployments"}` (matching the resource the webhook registered for),
	// and `requestResource: {group:"apps", version:"v1beta1", resource:"deployments"}` (indicating the resource of the original API request).
	//
	RequestResource *metav1.GroupVersionResource `json:"requestResource,omitempty"`

	// RequestSubResource is the name of the subresource of the original API request, if any (for example, "status" or "scale")
	// If this is specified and differs from the value in "subResource", an equivalent match and conversion was performed.
	RequestSubResource string `json:"requestSubResource,omitempty"`

	// UserInfo is information about the requesting user
	UserInfo authenticationv1.UserInfo `json:"userInfo"`

	// Name is the name of the object as presented in the request.  On a CREATE operation, the client may omit name and
	// rely on the server to generate the name.  If that is the case, this field will contain an empty string.
	Name string `json:"name,omitempty"`

	// Namespace is the namespace associated with the request (if any).
	Namespace string `json:"namespace,omitempty"`

	// Operation is the operation being performed. This may be different than the operation
	// requested. e.g. a patch can result in either a CREATE or UPDATE Operation.
	Operation string `json:"operation"`

	// Object is the object from the incoming request.
	Object runtime.RawExtension `json:"object,omitempty"`

	// OldObject is the existing object. Only populated for DELETE and UPDATE requests.
	OldObject runtime.RawExtension `json:"oldObject,omitempty"`

	// DryRun indicates that modifications will definitely not be persisted for this request.
	// Defaults to false.
	DryRun *bool `json:"dryRun,omitempty"`

	// Options is the operation option structure of the operation being performed.
	// e.g. `meta.k8s.io/v1.DeleteOptions` or `meta.k8s.io/v1.CreateOptions`. This may be
	// different than the options the caller provided. e.g. for a patch request the performed
	// Operation might be a CREATE, in which case the Options will a
	// `meta.k8s.io/v1.CreateOptions` even though the caller provided `meta.k8s.io/v1.PatchOptions`.
	Options runtime.RawExtension `json:"options,omitempty"`
}

// AdmissionResponse describes an admission response.
type AdmissionResponse struct {

	// UID is an identifier for the individual request/response.
	// This should be copied over from the corresponding AdmissionRequest.
	UID types.UID `json:"uid"`

	// Allowed indicates whether or not the admission request was permitted.
	Allowed bool `json:"allowed"`

	// Result contains extra details into why an admission request was denied.
	// This field IS NOT consulted in any way if "Allowed" is "true".
	Result *metav1.Status `json:"status,omitempty"`

	// The patch body. Currently we only support "JSONPatch" which implements RFC 6902.
	Patch []byte `json:"patch,omitempty"`

	// The type of Patch. Currently we only allow "JSONPatch".
	PatchType *string `json:"patchType,omitempty"`

	// AuditAnnotations is an unstructured key value map set by remote admission controller (e.g. error=image-blacklisted).
	// MutatingAdmissionWebhook and ValidatingAdmissionWebhook admission controller will prefix the keys with
	// admission webhook name (e.g. imagepolicy.example.com/error=image-blacklisted). AuditAnnotations will be provided by
	// the admission webhook to add additional context to the audit log for this request.
	AuditAnnotations map[string]string `json:"auditAnnotations,omitempty"`

	// warnings is a list of warning messages to return to the requesting API client.
	// Warning messages describe a problem the client making the API request should correct or be aware of.
	// Limit warnings to 120 characters if possible.
	// Warnings over 256 characters and large numbers of warnings may be truncated.
	Warnings []string `json:"warnings,omitempty"`
}

func AdmissionReviewKubeToAdapter(object runtime.Object) (*AdmissionReview, error) {
	var typeMeta metav1.TypeMeta
	var req *AdmissionRequest
	var resp *AdmissionResponse
	switch obj := object.(type) {
	case *kubeApiAdmissionv1beta1.AdmissionReview:
		typeMeta = obj.TypeMeta
		arv1beta1Response := obj.Response
		arv1beta1Request := obj.Request
		if arv1beta1Response != nil {
			resp = &AdmissionResponse{
				UID:      arv1beta1Response.UID,
				Allowed:  arv1beta1Response.Allowed,
				Result:   arv1beta1Response.Result,
				Patch:    arv1beta1Response.Patch,
				Warnings: arv1beta1Response.Warnings,
			}
			if arv1beta1Response.PatchType != nil {
				patchType := string(*arv1beta1Response.PatchType)
				resp.PatchType = &patchType
			}
		}
		if arv1beta1Request != nil {
			req = &AdmissionRequest{
				UID:       arv1beta1Request.UID,
				Kind:      arv1beta1Request.Kind,
				Resource:  arv1beta1Request.Resource,
				UserInfo:  arv1beta1Request.UserInfo,
				Name:      arv1beta1Request.Name,
				Namespace: arv1beta1Request.Namespace,
				Operation: string(arv1beta1Request.Operation),
				Object:    arv1beta1Request.Object,
				OldObject: arv1beta1Request.OldObject,
			}
		}

	case *kubeApiAdmissionv1.AdmissionReview:
		typeMeta = obj.TypeMeta
		arv1Response := obj.Response
		arv1Request := obj.Request
		if arv1Response != nil {
			resp = &AdmissionResponse{
				UID:      arv1Response.UID,
				Allowed:  arv1Response.Allowed,
				Result:   arv1Response.Result,
				Patch:    arv1Response.Patch,
				Warnings: arv1Response.Warnings,
			}
			if arv1Response.PatchType != nil {
				patchType := string(*arv1Response.PatchType)
				resp.PatchType = &patchType
			}
		}

		if arv1Request != nil {
			req = &AdmissionRequest{
				UID:       arv1Request.UID,
				Kind:      arv1Request.Kind,
				Resource:  arv1Request.Resource,
				UserInfo:  arv1Request.UserInfo,
				Name:      arv1Request.Name,
				Namespace: arv1Request.Namespace,
				Operation: string(arv1Request.Operation),
				Object:    arv1Request.Object,
				OldObject: arv1Request.OldObject,
			}
		}

	default:
		return nil, fmt.Errorf("unsupported type :%v", object.GetObjectKind())
	}

	return &AdmissionReview{
		TypeMeta: typeMeta,
		Request:  req,
		Response: resp,
	}, nil
}

func AdmissionReviewAdapterToKube(ar *AdmissionReview, apiVersion string) runtime.Object {
	var res runtime.Object
	arRequest := ar.Request
	arResponse := ar.Response
	if apiVersion == "" {
		apiVersion = admissionAPIV1beta1
	}
	switch apiVersion {
	case admissionAPIV1beta1:
		arv1beta1 := kubeApiAdmissionv1beta1.AdmissionReview{}
		if arRequest != nil {
			arv1beta1.Request = &kubeApiAdmissionv1beta1.AdmissionRequest{
				UID:                arRequest.UID,
				Kind:               arRequest.Kind,
				Resource:           arRequest.Resource,
				SubResource:        arRequest.SubResource,
				Name:               arRequest.Name,
				Namespace:          arRequest.Namespace,
				RequestKind:        arRequest.RequestKind,
				RequestResource:    arRequest.RequestResource,
				RequestSubResource: arRequest.RequestSubResource,
				Operation:          kubeApiAdmissionv1beta1.Operation(arRequest.Operation),
				UserInfo:           arRequest.UserInfo,
				Object:             arRequest.Object,
				OldObject:          arRequest.OldObject,
				DryRun:             arRequest.DryRun,
				Options:            arRequest.Options,
			}
		}
		if arResponse != nil {
			var patchType *kubeApiAdmissionv1beta1.PatchType
			if arResponse.PatchType != nil {
				patchType = (*kubeApiAdmissionv1beta1.PatchType)(arResponse.PatchType)
			}
			arv1beta1.Response = &kubeApiAdmissionv1beta1.AdmissionResponse{
				UID:              arResponse.UID,
				Allowed:          arResponse.Allowed,
				Result:           arResponse.Result,
				Patch:            arResponse.Patch,
				PatchType:        patchType,
				AuditAnnotations: arResponse.AuditAnnotations,
				Warnings:         arResponse.Warnings,
			}
		}
		arv1beta1.TypeMeta = ar.TypeMeta
		res = &arv1beta1
	case admissionAPIV1:
		arv1 := kubeApiAdmissionv1.AdmissionReview{}
		if arRequest != nil {
			arv1.Request = &kubeApiAdmissionv1.AdmissionRequest{
				UID:                arRequest.UID,
				Kind:               arRequest.Kind,
				Resource:           arRequest.Resource,
				SubResource:        arRequest.SubResource,
				Name:               arRequest.Name,
				Namespace:          arRequest.Namespace,
				RequestKind:        arRequest.RequestKind,
				RequestResource:    arRequest.RequestResource,
				RequestSubResource: arRequest.RequestSubResource,
				Operation:          kubeApiAdmissionv1.Operation(arRequest.Operation),
				UserInfo:           arRequest.UserInfo,
				Object:             arRequest.Object,
				OldObject:          arRequest.OldObject,
				DryRun:             arRequest.DryRun,
				Options:            arRequest.Options,
			}
		}
		if arResponse != nil {
			var patchType *kubeApiAdmissionv1.PatchType
			if arResponse.PatchType != nil {
				patchType = (*kubeApiAdmissionv1.PatchType)(arResponse.PatchType)
			}
			arv1.Response = &kubeApiAdmissionv1.AdmissionResponse{
				UID:              arResponse.UID,
				Allowed:          arResponse.Allowed,
				Result:           arResponse.Result,
				Patch:            arResponse.Patch,
				PatchType:        patchType,
				AuditAnnotations: arResponse.AuditAnnotations,
				Warnings:         arResponse.Warnings,
			}
		}
		arv1.TypeMeta = ar.TypeMeta
		res = &arv1
	}
	return res
}
