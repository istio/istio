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
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/scopes"

	"istio.io/istio/pkg/test/framework"
)

// step to be executed as part of a test.
type step interface {
	// Run this step.
	Run(ctx Context)
}

// Builder builds a test of a documented workflow from http://istio.io.
type Builder struct {
	steps []step
}

// NewBuilder returns an instance of an example test
func NewBuilder() *Builder {
	return &Builder{}
}

// RunScript adds a directive to run a script
func (e *Builder) RunScript(s FileSelector, verifier Verifier) *Builder {
	e.steps = append(e.steps, newScriptStep(s, verifier))
	return e
}

//WaitForPods adds a directive to wait for pods provided by a PodFunc to deploy
func (e *Builder) WaitForPods(fetchFunc KubePodFetchFunc) *Builder {
	e.steps = append(e.steps, newPodWaitStep(fetchFunc))
	return e
}

// Apply applies an existing file specified by path where the path is relative
// to the root of the Istio source tree.
func (e *Builder) Apply(namespace string, path FileSelector) *Builder {
	e.steps = append(e.steps, newApplyYamlStep(namespace, path))
	return e
}

// Delete deletes an existing file
func (e *Builder) Delete(namespace string, path FileSelector) *Builder {
	e.steps = append(e.steps, newDeleteYamlStep(namespace, path))
	return e
}

// Exec registers a callback to be invoked synchronously. This is typically used for
// validation logic to ensure command-lines worked as intended
func (e *Builder) Exec(fn testFn) *Builder {
	e.steps = append(e.steps, newFunctionStep(fn))
	return e
}

// Build a run function for the test
func (e *Builder) Build() func(ctx framework.TestContext) {
	return func(ctx framework.TestContext) {
		scopes.CI.Infof("Executing test %s (%d steps)", ctx.Name(), len(e.steps))

		kubeEnv, ok := ctx.Environment().(*kube.Environment)
		if !ok {
			ctx.Fatalf("test framework unable to get Kubernetes environment")
		}

		outputDir := ctx.CreateDirectoryOrFail("output")

		eCtx := Context{
			TestContext: ctx,
			Env:         kubeEnv,
			OutputDir:   outputDir,
		}

		for _, step := range e.steps {
			step.Run(eCtx)
		}
	}
}
