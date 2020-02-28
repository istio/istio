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

package istioctl

import (
	"regexp"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework/label"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
)

var (
	inst istio.Instance
)

// This test requires `--istio.test.env=kube` because it tests istioctl managing k8s webhook configurations.
func TestMain(m *testing.M) {
	framework.
		NewSuite("istioctl_webhook_test", m).
		Label(label.CustomSetup).
		RequireEnvironment(environment.Kube).
		// Deploy Istio
		SetupOnEnv(environment.Kube, istio.Setup(&inst, setupConfig)).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	// Helm values from install/kubernetes/helm/istio/test-values/values-istio-dns-cert.yaml
	cfg.ValuesFile = "test-values/values-istio-dns-cert.yaml"
	cfg.Values["global.operatorManageWebhooks"] = "true"
}

// TestWebhookManagement tests "istioctl experimental post-install webhook" command.
func TestWebhookManagement(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			ctx.Skip("https://github.com/istio/istio/issues/21574")
			cfg := inst.Settings()
			if cfg.IsIstiodEnabled() {
				ctx.Skip("TODO(github.com/istio/istio/issues/20289)")
			}

			// Test that webhook configurations are enabled through istioctl successfully.
			args := []string{"experimental", "post-install", "webhook", "enable", "--validation", "--webhook-secret",
				"dns.istio-galley-service-account", "--namespace", "istio-system", "--validation-path", "./config/galley-webhook.yaml",
				"--injection-path", "./config/sidecar-injector-webhook.yaml"}
			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})
			output, fErr := istioCtl.Invoke(args)
			if fErr != nil {
				t.Fatalf("error returned for 'istioctl %s': %v", strings.Join(args, " "), fErr)
			}

			// Check that the webhook configurations are successful
			expectedRegexps := []*regexp.Regexp{
				regexp.MustCompile(`finished reading cert`),
				regexp.MustCompile(`create webhook configuration istio-galley`),
				regexp.MustCompile(`create webhook configuration istio-sidecar-injector`),
				regexp.MustCompile(`webhook configurations have been enabled`),
			}
			for _, regexp := range expectedRegexps {
				if !regexp.MatchString(output) {
					t.Fatalf("output didn't match for 'istioctl %s'\n got %v\nwant: %v",
						strings.Join(args, " "), output, regexp)
				}
			}

			// Test that webhook statuses returned by running istioctl are as expected.
			args = []string{"experimental", "post-install", "webhook", "status"}
			istioCtl = istioctl.NewOrFail(t, ctx, istioctl.Config{})
			output, fErr = istioCtl.Invoke(args)
			if fErr != nil {
				t.Fatalf("error returned for 'istioctl %s': %v", strings.Join(args, " "), fErr)
			}

			// Check that the webhook statuses are as expected
			expectedRegexps = []*regexp.Regexp{
				regexp.MustCompile(`ValidatingWebhookConfiguration istio-galley is`),
				regexp.MustCompile(`MutatingWebhookConfiguration istio-sidecar-injector is`),
			}
			for _, regexp := range expectedRegexps {
				if !regexp.MatchString(output) {
					t.Fatalf("output didn't match for 'istioctl %s'\n got %v\nwant: %v",
						strings.Join(args, " "), output, regexp)
				}
			}

			// Currently, unable to test disabling webhooks because the disable command requires
			// user interaction: "Are you sure to delete webhook configuration(s)?", the deletion
			// will only proceed after user entering "yes".
		})
}
