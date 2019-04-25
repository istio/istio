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
Package writer provides method to provision and persist the certificates.

It will create the certificates if they don't exist.
It will ensure the certificates are valid and not expiring. If not, it will recreate them.

Create a CertWriter that can write the certificate to secret

	writer, err := NewSecretCertWriter(SecretCertWriterOptions{
		Secret: types.NamespacedName{Namespace: "foo", Name: "bar"},
		Client: client,
	})
	if err != nil {
		// handler error
	}

Create a CertWriter that can write the certificate to the filesystem.

	writer, err := NewFSCertWriter(FSCertWriterOptions{
		Path: "path/to/cert/",
	})
	if err != nil {
		// handler error
	}

Provision the certificates using the CertWriter. The certificate will be available in the desired secret or
the desired path.

	// writer can be either one of the CertWriters created above
	certs, changed, err := writer.EnsureCerts("admissionwebhook.k8s.io", false)
	if err != nil {
		// handler error
	}

Inject necessary information given the objects.

	err = writer.Inject(objs...)
	if err != nil {
		// handler error
	}
*/
package writer

import (
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.KBLog.WithName("admission").WithName("cert").WithName("writer")
