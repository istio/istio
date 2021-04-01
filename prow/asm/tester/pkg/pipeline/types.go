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

package pipeline

import (
	"istio.io/istio/prow/asm/tester/interface/types"
	"istio.io/istio/prow/asm/tester/pkg/pipeline/env"
	"istio.io/istio/prow/asm/tester/pkg/pipeline/system"
	"istio.io/istio/prow/asm/tester/pkg/pipeline/tests"
	"istio.io/istio/prow/asm/tester/pkg/resource"
)

type ASMPipelineTester struct {
	resource.Settings
}

// Ensure ASMPipelineTester implements the interfaces
var _ types.BasePipelineTester = (*ASMPipelineTester)(nil)
var _ types.LifecycleEnv = (*ASMPipelineTester)(nil)
var _ types.LifecycleTests = (*ASMPipelineTester)(nil)

func (apt *ASMPipelineTester) SetupEnv() error {
	return env.Setup(&apt.Settings)
}

func (apt *ASMPipelineTester) TeardownEnv() error {
	return env.Teardown(&apt.Settings)
}

func (apt *ASMPipelineTester) SetupSystem() error {
	return system.Setup(&apt.Settings)
}

func (apt *ASMPipelineTester) TeardownSystem() error {
	return system.Teardown(&apt.Settings)
}

func (apt *ASMPipelineTester) SetupTests() error {
	return tests.Setup(&apt.Settings)
}

func (apt *ASMPipelineTester) TeardownTests() error {
	return tests.Teardown(&apt.Settings)
}

func (apt *ASMPipelineTester) RunTests() error {
	return tests.Run(&apt.Settings)
}
