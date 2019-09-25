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
	"testing"

	"istio.io/pkg/log"
)

const (
	expectCTHeader = "Expect-CT" // https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Expect-CT
)

func TestTLSOrigination(t *testing.T) {
	// This list is ordered so that cases that use the same configuration are adjacent. This is
	// done to avoid applying config changes more than necessary.
	cases := []struct {
		name              string
		config            string
		url               string
		shouldBeReachable bool
		shouldBeTLS       bool
	}{
		// use www.cloudflare.com as cloudflare.com results in 301
		{
			name:              "REACHABLE_WITHOUT_TLS_www.cloudflare.com_80_over_cloudflare_80",
			config:            "testdata/networking/v1alpha3/service-entry-cloudflare.yaml",
			url:               "http://www.cloudflare.com",
			shouldBeReachable: true,
			shouldBeTLS:       false,
		},
		{
			name:              "REACHABLE_WITH_TLS_www.cloudflare.com_443_over_cloudflare_443",
			config:            "testdata/networking/v1alpha3/service-entry-cloudflare.yaml",
			url:               "https://www.cloudflare.com",
			shouldBeReachable: true,
			shouldBeTLS:       false,
		},
		{
			name:              "REACHABLE_WITH_TLS_ORIGINATION_www.cloudflare.com_443_over_cloudflare_80",
			config:            "testdata/networking/v1alpha3/tls-origination-cloudflare.yaml",
			url:               "http://www.cloudflare.com",
			shouldBeReachable: true,
			shouldBeTLS:       true,
		},
		{
			name:              "UNREACHABLE_www.google.com_443_over_cloudflare_443",
			config:            "testdata/networking/v1alpha3/tls-origination-cloudflare.yaml",
			url:               "https://www.google.com",
			shouldBeReachable: false,
			shouldBeTLS:       false,
		},
		{
			name:              "UNREACHABLE_www.google.com_80_over_cloudflare_443",
			config:            "testdata/networking/v1alpha3/tls-origination-cloudflare.yaml",
			url:               "http://www.google.com",
			shouldBeReachable: false,
			shouldBeTLS:       false,
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

			for cluster := range tc.Kube.Clusters {
				// Make the requests and verify the reachability
				for _, src := range []string{"a"} {
					runRetriableTest(t, "from_"+src, 3, func() error {
						resp := ClientRequest(cluster, src, cs.url, 1, "")
						reachable := resp.IsHTTPOk()
						withTLS := false

						// cloudflare.com responds with "Expect-CT" header when accessed by TLS
						if _, hasExpectCTHeader := resp.Headers[expectCTHeader]; hasExpectCTHeader {
							withTLS = true
						}

						if reachable && !cs.shouldBeReachable {
							return fmt.Errorf("%s is reachable from %s (should be unreachable)", cs.url, src)
						}
						if !reachable && cs.shouldBeReachable {
							log.Errorf("%s is not reachable while it should be reachable from %s", cs.url, src)
							return errAgain
						}

						if withTLS && !cs.shouldBeTLS {
							return fmt.Errorf("%s is accessed by TLS from %s (should be without TLS)", cs.url, src)
						}
						if !withTLS && cs.shouldBeTLS {
							return fmt.Errorf("%s is accessed without TLS from %s while it should be with TLS", cs.url, src)

						}

						return nil
					})
				}
			}
		})
	}
}
