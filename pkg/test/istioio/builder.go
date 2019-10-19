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
	"os"
	"path/filepath"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/scopes"

	"istio.io/istio/pkg/test/framework"
)

// Step builds a step of the test pipeline.
type Step interface {
	// run this step.
	run(ctx Context)
}

// Builder builds a test of a documented workflow from http://istio.io.
type Builder struct {
	steps        []Step
	cleanupSteps []Step
}

// NewBuilder returns an instance of an example test
func NewBuilder() *Builder {
	return &Builder{}
}

// Add a step to be run.
func (b *Builder) Add(steps ...Step) *Builder {
	b.steps = append(b.steps, steps...)
	return b
}

// Defer registers a function to be executed when the test completes.
func (b *Builder) Defer(steps ...Step) *Builder {
	b.cleanupSteps = append(b.cleanupSteps, steps...)
	return b
}

// Build a run function for the test
func (b *Builder) Build() func(ctx framework.TestContext) {
	return func(ctx framework.TestContext) {
		scopes.CI.Infof("Executing test %s (%d steps)", ctx.Name(), len(b.steps))

		kubeEnv, ok := ctx.Environment().(*kube.Environment)
		if !ok {
			ctx.Fatalf("test framework unable to get Kubernetes environment")
		}

		snippetsFile, err := os.Create(filepath.Join(ctx.WorkDir(), "snippets.txt"))
		if err != nil {
			ctx.Fatalf("failed creating snippets file: %v", err)
		}
		defer func() { _ = snippetsFile.Close() }()

		eCtx := Context{
			TestContext:  ctx,
			Env:          kubeEnv,
			SnippetsFile: snippetsFile,
		}

		// Run cleanup functions at the end.
		defer func() {
			for _, step := range b.cleanupSteps {
				step.run(eCtx)
			}
		}()

		for _, step := range b.steps {
			step.run(eCtx)
		}
	}
}
