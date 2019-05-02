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

package webhook

import (
	"context"

	admissionregistration "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type mutateFn func(current, desired *runtime.Object) error

var serviceFn = func(current, desired *runtime.Object) error {
	typedC := (*current).(*corev1.Service)
	typedD := (*desired).(*corev1.Service)
	typedC.Spec.Selector = typedD.Spec.Selector
	return nil
}

var mutatingWebhookConfigFn = func(current, desired *runtime.Object) error {
	typedC := (*current).(*admissionregistration.MutatingWebhookConfiguration)
	typedD := (*desired).(*admissionregistration.MutatingWebhookConfiguration)
	typedC.Webhooks = typedD.Webhooks
	return nil
}

var validatingWebhookConfigFn = func(current, desired *runtime.Object) error {
	typedC := (*current).(*admissionregistration.ValidatingWebhookConfiguration)
	typedD := (*desired).(*admissionregistration.ValidatingWebhookConfiguration)
	typedC.Webhooks = typedD.Webhooks
	return nil
}

var genericFn = func(current, desired *runtime.Object) error {
	*current = *desired
	return nil
}

// createOrReplaceHelper creates the object if it doesn't exist;
// otherwise, it will replace it.
// When replacing, fn  should know how to preserve existing fields in the object GET from the APIServer.
// TODO: use the helper in #98 when it merges.
func createOrReplaceHelper(c client.Client, obj runtime.Object, fn mutateFn) error {
	if obj == nil {
		return nil
	}
	err := c.Create(context.Background(), obj)
	if apierrors.IsAlreadyExists(err) {
		// TODO: retry mutiple times with backoff if necessary.
		existing := obj.DeepCopyObject()
		objectKey, err := client.ObjectKeyFromObject(obj)
		if err != nil {
			return err
		}
		err = c.Get(context.Background(), objectKey, existing)
		if err != nil {
			return err
		}
		err = fn(&existing, &obj)
		if err != nil {
			return err
		}
		return c.Update(context.Background(), existing)
	}
	return err
}

// createOrReplace creates the object if it doesn't exist;
// otherwise, it will replace it.
// When replacing, it knows how to preserve existing fields in the object GET from the APIServer.
// It currently only support MutatingWebhookConfiguration, ValidatingWebhookConfiguration and Service.
// For other kinds, it uses genericFn to replace the whole object.
func createOrReplace(c client.Client, obj runtime.Object) error {
	if obj == nil {
		return nil
	}
	switch obj.(type) {
	case *admissionregistration.MutatingWebhookConfiguration:
		return createOrReplaceHelper(c, obj, mutatingWebhookConfigFn)
	case *admissionregistration.ValidatingWebhookConfiguration:
		return createOrReplaceHelper(c, obj, validatingWebhookConfigFn)
	case *corev1.Service:
		return createOrReplaceHelper(c, obj, serviceFn)
	default:
		return createOrReplaceHelper(c, obj, genericFn)
	}
}

func batchCreateOrReplace(c client.Client, objs ...runtime.Object) error {
	for i := range objs {
		err := createOrReplace(c, objs[i])
		if err != nil {
			return err
		}
	}
	return nil
}
