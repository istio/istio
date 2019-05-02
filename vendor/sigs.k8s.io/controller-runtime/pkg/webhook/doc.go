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
Package webhook provides methods to build and bootstrap a webhook server.

Currently, it only supports admission webhooks. It will support CRD conversion webhooks in the near future.

Build webhooks

	// mgr is the manager that runs the server.
	webhook1, err := NewWebhookBuilder().
		Name("foo.k8s.io").
		Mutating().
		Path("/mutating-pods").
		Operations(admissionregistrationv1beta1.Create).
		ForType(&corev1.Pod{}).
		WithManager(mgr).
		Handlers(mutatingHandler1, mutatingHandler2).
		Build()
	if err != nil {
		// handle error
	}

	webhook2, err := NewWebhookBuilder().
		Name("bar.k8s.io").
		Validating().
		Path("/validating-deployment").
		Operations(admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update).
		ForType(&appsv1.Deployment{}).
		WithManager(mgr).
		Handlers(validatingHandler1).
		Build()
	if err != nil {
		// handle error
	}

Create a webhook server.

	as, err := NewServer("baz-admission-server", mgr, ServerOptions{
		CertDir: "/tmp/cert",
		BootstrapOptions: &BootstrapOptions{
			Secret: &apitypes.NamespacedName{
				Namespace: "default",
				Name:      "foo-admission-server-secret",
			},
			Service: &Service{
				Namespace: "default",
				Name:      "foo-admission-server-service",
				// Selectors should select the pods that runs this webhook server.
				Selectors: map[string]string{
					"app": "foo-admission-server",
				},
			},
		},
	})
	if err != nil {
		// handle error
	}

Register the webhooks in the server.

	err = as.Register(webhook1, webhook2)
	if err != nil {
		// handle error
	}

Start the server by starting the manager

	err := mrg.Start(signals.SetupSignalHandler())
	if err != nil {
		// handle error
	}
*/
package webhook

import (
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.KBLog.WithName("webhook")
