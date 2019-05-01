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

package framework

import (
	"testing"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"

	"istio.io/istio/pkg/test/framework"
)

var (
	i   istio.Instance
	env *kube.Environment
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("framework_test", m).
		Label(label.Presubmit).
		// The deployment must work. If you're breaking this, you'll break many integration tests.
		SetupOnEnv(environment.Kube, istio.Setup(&i, nil)).
		SetupOnEnv(environment.Kube, func(ctx resource.Context) error {
			env = ctx.Environment().(*kube.Environment)
			return nil
		}).
		Run()
}
