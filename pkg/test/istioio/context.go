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

package istioio

import (
	"io"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
)

// Context for the currently executing test.
type Context struct {
	framework.TestContext
	snippetFile io.Writer

	// Maintain the set of all snippets added so far to avoid duplicates.
	snippetMap map[string]string
}

// addSnippet adds the content of the given snippet to the snippet file.
func (ctx Context) addSnippet(snippetName, inputName, content string) {
	// Ensure we don't duplicate snippet names.
	if prevInput := ctx.snippetMap[snippetName]; prevInput != "" {
		ctx.Fatalf("Duplicate snippet %s in input %s. Previous input: %s.", snippetName, inputName, prevInput)
	}
	ctx.snippetMap[snippetName] = inputName

	// Write the snippet.
	if _, err := ctx.snippetFile.Write([]byte(content)); err != nil {
		ctx.Fatalf("Failed writing snippet %s: %v", snippetName, err)
	}
}

// KubeEnv casts the test environment as a *kube.Environment. If the cast fails, fails the test.
func (ctx Context) KubeEnv() *kube.Environment {
	e, ok := ctx.Environment().(*kube.Environment)
	if !ok {
		ctx.Fatalf("test framework unable to get Kubernetes environment")
	}
	return e
}
