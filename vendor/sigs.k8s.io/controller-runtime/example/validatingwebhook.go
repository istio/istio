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

package main

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
)

// podValidator validates Pods
type podValidator struct {
	client  client.Client
	decoder types.Decoder
}

// Implement admission.Handler so the controller can handle admission request.
var _ admission.Handler = &podValidator{}

// podValidator admits a pod iff a specific annotation exists.
func (v *podValidator) Handle(ctx context.Context, req types.Request) types.Response {
	pod := &corev1.Pod{}

	err := v.decoder.Decode(req, pod)
	if err != nil {
		return admission.ErrorResponse(http.StatusBadRequest, err)
	}

	allowed, reason, err := v.validatePodsFn(ctx, pod)
	if err != nil {
		return admission.ErrorResponse(http.StatusInternalServerError, err)
	}
	return admission.ValidationResponse(allowed, reason)
}

func (v *podValidator) validatePodsFn(ctx context.Context, pod *corev1.Pod) (bool, string, error) {
	key := "example-mutating-admission-webhook"
	anno, found := pod.Annotations[key]
	switch {
	case !found:
		return found, fmt.Sprintf("failed to find annotation with key: %q", key), nil
	case found && anno == "foo":
		return found, "", nil
	case found && anno != "foo":
		return false,
			fmt.Sprintf("the value associate with key %q is expected to be %q, but got %q", key, "foo", anno), nil
	}
	return false, "", nil
}

// podValidator implements inject.Client.
// A client will be automatically injected.
var _ inject.Client = &podValidator{}

// InjectClient injects the client.
func (v *podValidator) InjectClient(c client.Client) error {
	v.client = c
	return nil
}

// podValidator implements inject.Decoder.
// A decoder will be automatically injected.
var _ inject.Decoder = &podValidator{}

// InjectDecoder injects the decoder.
func (v *podValidator) InjectDecoder(d types.Decoder) error {
	v.decoder = d
	return nil
}
