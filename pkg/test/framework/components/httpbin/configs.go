// Copyright 2019 Istio Authors
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

package httpbin

import (
	"path"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/util/yml"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/scopes"
)

// ConfigFile represents config yaml files for different httpbin scenarios.
type ConfigFile string

const (
	// NetworkingHttpbinGateway uses "httpbin-gateway.yaml"
	NetworkingHttpbinGateway ConfigFile = "httpbin-gateway.yaml"
)

// LoadWithNamespaceOrFail loads a httpbin configuration file for the namespace and returns its contents.
func (l ConfigFile) LoadWithNamespaceOrFail(t testing.TB, namespace string) string {
	t.Helper()
	p := path.Join(env.HttpbinRoot, string(l))

	content, err := test.ReadConfigFile(p)
	if err != nil {
		t.Fatalf("unable to load config %s at %v, err:%v", l, p, err)
	}
	if namespace != "" {
		content, err = yml.ApplyNamespace(content, namespace)
		if err != nil {
			t.Fatalf("cannot apply namespace config to httpbin yaml content: %v", content)
		}
		content = replaceHttpbinAppAddressWithFQDNAddress(content, namespace)
	}

	scopes.Framework.Debugf("Loaded Httpbin file: %s\n%s\n", p, content)
	return content
}

func replaceHttpbinAppAddressWithFQDNAddress(fileContent, namespace string) string {
	content := fileContent
	content = strings.Replace(content, "host: httpbin", "host: httpbin."+namespace+".svc.cluster.local", -1)
	return content
}
