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
)

func TestServiceEntry(t *testing.T) {
	// This list is ordered so that cases that use the same egress rule are adjacent. This is
	// done to avoid applying config changes more than necessary.
	cases := []struct {
		name              string
		config            string
		url               string
		shouldBeReachable bool
	}{
		{
			name:              "REACHABLE_www.google.com_over_google_80",
			config:            "testdata/networking/v1alpha3/service-entry-google.yaml",
			url:               "http://www.google.com",
			shouldBeReachable: true,
		},
		{
			name:              "UNREACHABLE_bing.com_over_google_80",
			config:            "testdata/networking/v1alpha3/service-entry-google.yaml",
			url:               "http://bing.com",
			shouldBeReachable: false,
		},
		{
			name:              "REACHABLE_www.bing.com_over_bing_wildcard_80",
			config:            "testdata/networking/v1alpha3/service-entry-wildcard-bing.yaml",
			url:               "http://www.bing.com",
			shouldBeReachable: true,
		},
		{
			name:              "UNREACHABLE_bing.com_over_bing_wildcard_80",
			config:            "testdata/networking/v1alpha3/service-entry-wildcard-bing.yaml",
			url:               "http://bing.com",
			shouldBeReachable: false,
		},
		// See issue https://github.com/istio/istio/issues/7869
		//{
		//	name:              "REACHABLE_wikipedia.org_over_cidr_range",
		//	config:            "testdata/networking/v1alpha3/service-entry-tcp-wikipedia-cidr.yaml",
		//	url:               "https://www.wikipedia.org",
		//	shouldBeReachable: true,
		//},
		//{
		//	name:              "UNREACHABLE_google.com_over_cidr_range",
		//	config:            "testdata/networking/v1alpha3/service-entry-tcp-wikipedia-cidr.yaml",
		//	url:               "https://google.com",
		//	shouldBeReachable: false,
		//},
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
					runRetriableTest(t, cluster, "from_"+src, 3, func() error {
						resp := ClientRequest(cluster, src, cs.url, 1, "")
						reachable := resp.IsHTTPOk()
						if reachable && !cs.shouldBeReachable {
							return fmt.Errorf("%s is reachable from %s (should be unreachable)", cs.url, src)
						}
						if !reachable && cs.shouldBeReachable {
							return errAgain
						}

						return nil
					})
				}
			}
		})
	}
}
