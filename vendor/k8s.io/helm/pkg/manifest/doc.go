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

/*Package manifest contains tools for working with kubernetes manifests.

Much like other parts of helm, it does not generally require that the manifests
be correct yaml, so these functions can be run on broken manifests to aid in
user debugging
*/
package manifest // import "k8s.io/helm/pkg/manifest"
