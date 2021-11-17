// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Embed the core metadata file containing the collections as a resource
//go:generate go-bindata --nocompress --nometadata --pkg data -o builtin.gen.go builtin/

package data

// GetEndpoints returns Endpoints test data
func GetEndpoints() string {
	return string(MustAsset("builtin/endpoints.yaml"))
}

// GetNode returns Node test data
func GetNode() string {
	return string(MustAsset("builtin/node.yaml"))
}

// GetPod returns Pod test data
func GetPod() string {
	return string(MustAsset("builtin/pod.yaml"))
}

// GetService returns Service test data
func GetService() string {
	return string(MustAsset("builtin/service.yaml"))
}

// GetNamespace returns Namespace test data
func GetNamespace() string {
	return string(MustAsset("builtin/namespace.yaml"))
}

// GetIngress returns Ingress test data
func GetIngress() string {
	return string(MustAsset("builtin/ingress.yaml"))
}

// GetDeployment returns Deployment test data
func GetDeployment() string {
	return string(MustAsset("builtin/deployment.yaml"))
}
