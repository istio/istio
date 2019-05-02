/*
Copyright The Helm Authors.
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

/*Package provenance provides tools for establishing the authenticity of a chart.

In Helm, provenance is established via several factors. The primary factor is the
cryptographic signature of a chart. Chart authors may sign charts, which in turn
provide the necessary metadata to ensure the integrity of the chart file, the
Chart.yaml, and the referenced Docker images.

A provenance file is clear-signed. This provides cryptographic verification that
a particular block of information (Chart.yaml, archive file, images) have not
been tampered with or altered. To learn more, read the GnuPG documentation on
clear signatures:
https://www.gnupg.org/gph/en/manual/x135.html

The cryptography used by Helm should be compatible with OpenGPG. For example,
you should be able to verify a signature by importing the desired public key
and using `gpg --verify`, `keybase pgp verify`, or similar:

	$  gpg --verify some.sig
	gpg: Signature made Mon Jul 25 17:23:44 2016 MDT using RSA key ID 1FC18762
	gpg: Good signature from "Helm Testing (This key should only be used for testing. DO NOT TRUST.) <helm-testing@helm.sh>" [ultimate]
*/
package provenance // import "k8s.io/helm/pkg/provenance"
