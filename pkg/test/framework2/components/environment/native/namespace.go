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

package native

import (
	"testing"

	"istio.io/istio/pkg/test/framework2/core"
	"istio.io/istio/pkg/test/util/yml"
)

// nativeNamespace represents an imaginary namespace. It is tracked as a resource.
type nativeNamespace struct {
	id   core.ResourceID
	name string
}

var _ core.Namespace = &nativeNamespace{}
var _ core.Resource = &nativeNamespace{}

func (n *nativeNamespace) Name() string {
	return n.name
}

func (n *nativeNamespace) ID() core.ResourceID {
	return n.id
}

// Apply the namespace to the resources in the given yaml text.
func (n *nativeNamespace) Apply(yamlText string) (string, error) {
	return yml.ApplyNamespace(yamlText, n.name)
}

// Apply the namespace to the resources in the given yaml text, or fail test
func (n *nativeNamespace) ApplyOrFail(t *testing.T, yamlText string) string {
	t.Helper()
	y, err := n.Apply(yamlText)
	if err != nil {
		t.Fatalf("core.Namespace: %v", err)
	}

	return y
}
