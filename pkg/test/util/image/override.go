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

package image

import (
	"os"
	"strings"
)

const (
	DockerRegistry = "docker.io"
	GHCRRegistry   = "ghcr.io"
	IstioRegistry  = "registry.istio.io"
	K8sRegistry    = "registry.k8s.io"
	QuayRegistry   = "quay.io"

	DockerRegistryEnv = "DOCKER_REGISTRY"
	GHCRRegistryEnv   = "GHCR_REGISTRY"
	K8sRegistryEnv    = "K8S_REGISTRY"
	QuayRegistryEnv   = "QUAY_REGISTRY"
)

// FromRegistry assembles an image reference from a registry, repository, and optional tag.
func FromRegistry(defaultRegistry, envVar, repository, tag string) string {
	registry := strings.TrimSuffix(os.Getenv(envVar), "/")
	if registry == "" {
		registry = strings.TrimSuffix(defaultRegistry, "/")
	}
	if registry == "" {
		return ""
	}
	image := registry + "/" + strings.TrimPrefix(repository, "/")
	if tag != "" {
		image += ":" + tag
	}
	return image
}
