// Copyright 2017 Istio Authors
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

package pilot

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEgress(t *testing.T) {
	if !tc.Egress {
		t.Skipf("Skipping %s: egress=false", t.Name())
	}
	// egress rules are v1alpha1
	if !tc.V1alpha1 {
		t.Skipf("Skipping %s: v1alpha1=false", t.Name())
	}

	// This list is ordered so that cases that use the same egress rule are adjacent. This is
	// done to avoid applying config changes more than necessary.
	cases := []struct {
		name              string
		config            string
		url               string
		shouldBeReachable bool
	}{
		{
			name:   "REACHABLE_google.com_443",
			config: "testdata/v1alpha1/egress-rule-google.yaml",
			// Note that we're using http (not https). We're relying on Envoy to convert the outbound call to
			// TLS for us. This is currently the suggested way for the application to call an external TLS service.\
			// If the application uses TLS, then no metrics will be collected for the request.
			url:               "http://www.google.com:443",
			shouldBeReachable: true,
		},
		{
			name:              "REACHABLE_httpbin.org",
			config:            "testdata/v1alpha1/egress-rule-httpbin.yaml",
			url:               "http://httpbin.org/headers",
			shouldBeReachable: true,
		},
		{
			name:   "UNREACHABLE_httpbin.org_443",
			config: "testdata/v1alpha1/egress-rule-httpbin.yaml",
			// Note that we're using http (not https). We're relying on Envoy to convert the outbound call to
			// TLS for us. This is currently the suggested way for the application to call an external TLS service.\
			// If the application uses TLS, then no metrics will be collected for the request.
			url:               "http://httpbin.org:443/headers",
			shouldBeReachable: false,
		},
		{
			name:              "REACHABLE_www.httpbin.org",
			config:            "testdata/v1alpha1/egress-rule-wildcard-httpbin.yaml",
			url:               "http://www.httpbin.org/headers",
			shouldBeReachable: true,
		},
		{
			name:              "UNREACHABLE_httpbin.org",
			config:            "testdata/v1alpha1/egress-rule-wildcard-httpbin.yaml",
			url:               "http://httpbin.org/headers",
			shouldBeReachable: false,
		},
		{
			name:              "REACHABLE_wikipedia",
			config:            "testdata/v1alpha1/egress-rule-tcp-wikipedia-cidr.yaml",
			url:               "https://www.wikipedia.org",
			shouldBeReachable: true,
		},
		{
			name:              "UNREACHABLE_cnn",
			config:            "testdata/v1alpha1/egress-rule-tcp-wikipedia-cidr.yaml",
			url:               "https://cnn.com",
			shouldBeReachable: false,
		},
	}

	var cfgs *deployableConfig
	applyRuleFunc := func(t *testing.T, ruleYaml string) {
		configChange := cfgs == nil || cfgs.YamlFiles[0] != ruleYaml
		if configChange {
			// Delete the previous rule if there was one. No delay on the teardown, since we're going to apply
			// a delay when we push the new config.
			if cfgs != nil {
				if err := cfgs.TeardownNoDelay(); err != nil {
					t.Fatal(err)
				}
				cfgs = nil
			}

			// Apply the new rule
			cfgs = &deployableConfig{
				Namespace:  tc.Kube.Namespace,
				YamlFiles:  []string{ruleYaml},
				kubeconfig: tc.Kube.KubeConfig,
			}
			if err := cfgs.Setup(); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Upon function exit, delete the active rule.
	defer func() {
		if cfgs != nil {
			_ = cfgs.Teardown()
		}
	}()

	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			// Apply the rule
			applyRuleFunc(t, cs.config)

			// Make the requests and verify the reachability
			for _, src := range []string{"a", "b"} {
				runRetriableTest(t, src, 3, func() error {
					trace := fmt.Sprint(time.Now().UnixNano())
					resp := ClientRequest(src, cs.url, 1, fmt.Sprintf("-key Trace-Key -val %q", trace))
					reachable := resp.IsHTTPOk() && strings.Contains(resp.Body, trace)
					if reachable && !cs.shouldBeReachable {
						return fmt.Errorf("%s is reachable from %s (should be unreachable)", cs.url, src)
					}
					if !reachable && cs.shouldBeReachable {
						return errAgain
					}

					return nil
				})
			}
		})
	}
}
