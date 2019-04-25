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

/*
Package admission provides implementation for admission webhook and methods to implement admission webhook handlers.

The following snippet is an example implementation of mutating handler.

	type Mutator struct {
		client  client.Client
		decoder types.Decoder
	}

	func (m *Mutator) mutatePodsFn(ctx context.Context, pod *corev1.Pod) error {
		// your logic to mutate the passed-in pod.
	}

	func (m *Mutator) Handle(ctx context.Context, req types.Request) types.Response {
		pod := &corev1.Pod{}
		err := m.decoder.Decode(req, pod)
		if err != nil {
			return admission.ErrorResponse(http.StatusBadRequest, err)
		}
		// Do deepcopy before actually mutate the object.
		copy := pod.DeepCopy()
		err = m.mutatePodsFn(ctx, copy)
		if err != nil {
			return admission.ErrorResponse(http.StatusInternalServerError, err)
		}
		return admission.PatchResponse(pod, copy)
	}

	// InjectClient is called by the Manager and provides a client.Client to the Mutator instance.
	func (m *Mutator) InjectClient(c client.Client) error {
		h.client = c
		return nil
	}

	// InjectDecoder is called by the Manager and provides a types.Decoder to the Mutator instance.
	func (m *Mutator) InjectDecoder(d types.Decoder) error {
		h.decoder = d
		return nil
	}

The following snippet is an example implementation of validating handler.

	type Handler struct {
		client  client.Client
		decoder types.Decoder
	}

	func (v *Validator) validatePodsFn(ctx context.Context, pod *corev1.Pod) (bool, string, error) {
		// your business logic
	}

	func (v *Validator) Handle(ctx context.Context, req types.Request) types.Response {
		pod := &corev1.Pod{}
		err := h.decoder.Decode(req, pod)
		if err != nil {
			return admission.ErrorResponse(http.StatusBadRequest, err)
		}

		allowed, reason, err := h.validatePodsFn(ctx, pod)
		if err != nil {
			return admission.ErrorResponse(http.StatusInternalServerError, err)
		}
		return admission.ValidationResponse(allowed, reason)
	}

	// InjectClient is called by the Manager and provides a client.Client to the Validator instance.
	func (v *Validator) InjectClient(c client.Client) error {
		h.client = c
		return nil
	}

	// InjectDecoder is called by the Manager and provides a types.Decoder to the Validator instance.
	func (v *Validator) InjectDecoder(d types.Decoder) error {
		h.decoder = d
		return nil
	}
*/
package admission

import (
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.KBLog.WithName("admission")
