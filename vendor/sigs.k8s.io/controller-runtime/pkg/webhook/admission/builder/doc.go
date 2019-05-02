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
Package builder provides methods to build admission webhooks.

The following are 2 examples for building mutating webhook and validating webhook.

	webhook1, err := NewWebhookBuilder().
		Mutating().
		Operations(admissionregistrationv1beta1.Create).
		ForType(&corev1.Pod{}).
		WithManager(mgr).
		Handlers(mutatingHandler11, mutatingHandler12).
		Build()
	if err != nil {
		// handle error
	}

	webhook2, err := NewWebhookBuilder().
		Validating().
		Operations(admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update).
		ForType(&appsv1.Deployment{}).
		WithManager(mgr).
		Handlers(validatingHandler21).
		Build()
	if err != nil {
		// handle error
	}

Note: To build a webhook for a CRD, you need to ensure the manager uses the scheme that understands your CRD.
This is necessary, because if the scheme doesn't understand your CRD types, the decoder won't be able to decode
the CR object from the admission review request.

The following snippet shows how to register CRD types with manager's scheme.

	mgr, err := manager.New(cfg, manager.Options{})
	if err != nil {
		// handle error
	}
	// SchemeGroupVersion is group version used to register these objects
	SchemeGroupVersion = schema.GroupVersion{Group: "crew.k8s.io", Version: "v1"}
	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: SchemeGroupVersion}
	// Register your CRD types.
	SchemeBuilder.Register(&Kraken{}, &KrakenList{})
	// Register your CRD types with the manager's scheme.
	err = SchemeBuilder.AddToScheme(mgr.GetScheme())
	if err != nil {
		// handle error
	}

There are more options for configuring a webhook. e.g. Name, Path, FailurePolicy, NamespaceSelector.
Here is another example:

	webhook3, err := NewWebhookBuilder().
		Name("foo.example.com").
		Path("/mutatepods").
		Mutating().
		Operations(admissionregistrationv1beta1.Create).
		ForType(&corev1.Pod{}).
		FailurePolicy(admissionregistrationv1beta1.Fail).
		WithManager(mgr).
		Handlers(mutatingHandler31, mutatingHandler32).
		Build()
	if err != nil {
		// handle error
	}

For most users, we recommend to use Operations and ForType instead of Rules to construct a webhook,
since it is more intuitive and easier to pass the target operations to Operations method and
a empty target object to ForType method than passing a complex RuleWithOperations struct to Rules method.

Rules may be useful for some more advanced use cases like subresources, wildcard resources etc.
Here is an example:

	webhook4, err := NewWebhookBuilder().
		Validating().
		Rules(admissionregistrationv1beta1.RuleWithOperations{
			Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create},
			Rule: admissionregistrationv1beta1.Rule{
				APIGroups:   []string{"apps", "batch"},
				APIVersions: []string{"v1"},
				Resources:   []string{"*"},
			},
		}).
		WithManager(mgr).
		Handlers(validatingHandler41).
		Build()
	if err != nil {
		// handle error
	}
*/
package builder
