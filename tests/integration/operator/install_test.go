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
	"istio.io/istio/pkg/test/util/assert"
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

func TestReInstallAfterFailure(t *testing.T) {
	framework.NewTest(t).
		Features("installation.istioctl.install").
		Run(func(t framework.TestContext) {
			istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{})
			cs := t.Clusters().Default()
			t.Cleanup(func() {
				cleanupIstioResources(t, cs, istioCtl)
			})

			// Install with a fake tag to make the installation fail
			_, _, err := istioCtl.Invoke([]string{
				"install", "--skip-confirmation",
				"--set", "tag=0.20.0-faketag",
				"--readiness-timeout", "5s",
			})
			assert.Error(t, err)

			// Here we should have two activated webhooks, but dry-run should not report any error, which
			// means the re-installation can be done successfully.
			output, outErr := istioCtl.InvokeOrFail(t, []string{"install", "--dry-run"})
			if !strings.Contains(output, "Made this installation the default for injection and validation.") {
				t.Errorf("install expects to succeed but didn't")
			}
			assert.Equal(t, "", outErr)
		})
}
