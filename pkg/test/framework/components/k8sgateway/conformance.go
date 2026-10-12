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

package k8sgateway

import (
	"io/fs"
	"strings"
	"testing/fstest"

	"sigs.k8s.io/gateway-api/conformance"

	testimage "istio.io/istio/pkg/test/util/image"
)

const (
	defaultGatewayAPIRepository = "gateway-api"
	defaultCoreDNSRepository    = "coredns"
)

type conformanceManifestFS struct {
	source fs.FS
}

func (c conformanceManifestFS) Open(name string) (fs.File, error) {
	data, err := fs.ReadFile(c.source, name)
	if err != nil {
		return nil, err
	}
	manifest := string(data)
	for _, repository := range []string{defaultGatewayAPIRepository, defaultCoreDNSRepository} {
		manifest = strings.ReplaceAll(manifest,
			testimage.FromRegistry(testimage.K8sRegistry, "", repository, ""),
			testimage.FromRegistry(testimage.K8sRegistry, testimage.K8sRegistryEnv, repository, ""))
	}
	return fstest.MapFS{name: &fstest.MapFile{Data: []byte(manifest)}}.Open(name)
}

// ConformanceManifestFS returns the Gateway API conformance manifests with optional image registry overrides.
func ConformanceManifestFS() []fs.FS {
	return []fs.FS{conformanceManifestFS{source: &conformance.Manifests}}
}
