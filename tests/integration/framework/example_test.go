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

package framework

import (
	"testing"

	"istio.io/istio/tests/integration/framework/mycomponent"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	mc mycomponent.Instance
)

func mysetup(c resource.Context) error {
	// this function will be called as part of suite setup. You can do one-time setup here.
	// returning an error from here will cause the suite to fail all-together.

	// You can use the suite context to perform various operations. For example you can create folders or temp
	// folders as part of your operations.
	_, err := c.CreateTmpDirectory("example_foo")
	if err != nil {
		return err
	}

	// As part of your setup, you can create suite-level resources and track them. The "mc" resource will be
	// active as long as the suit is alive, as it is created within the context of suite-level setup.
	mc, err = mycomponent.New(c, mycomponent.Config{})
	if err != nil {
		return err
	}

	return nil
}

func setupKube(_ resource.Context) error {
	return nil
}

func TestStyle1(t *testing.T) {
	// Ideally, run your test code in a lambda. This ensures that the resources allocated in the context of the test
	// is cleaned up correctly.
	framework.Run(t, func(ctx framework.TestContext) {
		// You can use the framework.TestContext methods directly to interact with the framework.
		ctx.CreateTmpDirectoryOrFail("boo")

		// You can allocate components at the test level as well. mc2's life will be scoped to this lambda call.
		mc2 := mycomponent.NewOrFail(t, ctx, mycomponent.Config{DoStuffElegantly: true})
		_ = mc2

		// Ignore these, these are here to appease the linter
		_ = mc
		_ = i
	})
}

func TestStyle2(t *testing.T) {
	// You can specify additional constraints using the more verbose form
	framework.NewTest(t).
		Label(label.Postsubmit).
		Run(func(ctx framework.TestContext) {

			// This tests will run only on Kube environment as Presubmit. Note that the suite level requirements will
			// always have precedence.
			//
			// The labels at the suite and test label will be cumulative. For example, this suite is tagged with Presubmit
			// and the test is tagged with Postsubmit. In aggregate, this test has both Presubmit and Postsubmit labels.
		})
}
