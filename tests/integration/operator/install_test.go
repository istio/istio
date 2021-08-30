//go:build integ
// +build integ

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

package operator

import (
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
)

const InvalidRevision = "invalid revision specified"

type installTestCase struct {
	command   []string
	errString string
}

// TestInstallCommandInput tests istioctl install command with different input arguments
func TestInstallCommandInput(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.istioctl.install").
		Run(func(ctx framework.TestContext) {
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
			testCases := []installTestCase{
				{
					command:   []string{"install", "--dry-run", "--revision", ""},
					errString: InvalidRevision,
				},
				{
					command:   []string{"install", "--dry-run", "--revision", "1.8.0"},
					errString: InvalidRevision,
				},
				{
					command:   []string{"install", "--dry-run", "--set", "values.global.network=network1"},
					errString: "",
				},
			}
			for _, test := range testCases {
				_, actualError, _ := istioCtl.Invoke(test.command)
				if !strings.Contains(actualError, test.errString) {
					t.Errorf("istioctl install command expects to fail with error message: %s, but got: %s", test.errString, actualError)
				}
			}
		})
}
