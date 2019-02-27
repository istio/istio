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

	"istio.io/istio/pkg/log"
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
		// use www.google.com as google.com results in 301
		{
			name:              "REACHABLE_www.google.com_80_over_google_80",
			config:            "testdata/networking/v1alpha3/service-entry-google.yaml",
			url:               "http://www.google.com",
			shouldBeReachable: true,
		},
		{
			name:              "REACHABLE_www.google.com_443_over_google_443",
			config:            "testdata/networking/v1alpha3/service-entry-google.yaml",
			url:               "https://www.google.com",
			shouldBeReachable: true,
		},
		// use www.bing.com as bing.com results in 301.
		{
			name:              "UNREACHABLE_www.bing.com_443_over_google_443",
			config:            "testdata/networking/v1alpha3/service-entry-google.yaml",
			url:               "https://www.bing.com",
			shouldBeReachable: false,
		},
		{
			name:              "UNREACHABLE_www.bing.com_80_over_google_443",
			config:            "testdata/networking/v1alpha3/service-entry-google.yaml",
			url:               "http://www.bing.com",
			shouldBeReachable: false,
		},
		{
			name:              "REACHABLE_www.bing.com_80_over_bing_wildcard_80",
			config:            "testdata/networking/v1alpha3/service-entry-wildcard-bing.yaml",
			url:               "http://www.bing.com",
			shouldBeReachable: true,
		},
		// note this will get a 301 move response when reachable
		{
			name:              "UNREACHABLE_www_bing.com_443_over_bing_wildcard_80",
			config:            "testdata/networking/v1alpha3/service-entry-wildcard-bing.yaml",
			url:               "https://www.bing.com",
			shouldBeReachable: false,
		},
		// test resolution NONE
		{
			name:              "REACHABLE_www.wikipedia.org_443_over_wikipedia_cidr_range_443",
			config:            "testdata/networking/v1alpha3/service-entry-tcp-wikipedia-cidr.yaml",
			url:               "https://www.wikipedia.org",
			shouldBeReachable: true,
		},
		{
			name:              "UNREACHABLE_www.google.com_443_over_wikipedia_cidr_range_443",
			config:            "testdata/networking/v1alpha3/service-entry-tcp-wikipedia-cidr.yaml",
			url:               "https://www.google.com",
			shouldBeReachable: false,
		},
		{
			name:              "REACHABLE_en.wikipedia.org_443_over_wikipedia_wildcard_tls",
			config:            "testdata/networking/v1alpha3/wildcard-tls-wikipedia.yaml",
			url:               "https://en.wikipedia.org/wiki/Main_Page",
			shouldBeReachable: true,
		},
		{
			name:              "REACHABLE_de.wikipedia.org_443__over_wikipedia_wildcard_tls",
			config:            "testdata/networking/v1alpha3/wildcard-tls-wikipedia.yaml",
			url:               "https://de.wikipedia.org/wiki/Wikipedia:Hauptseite",
			shouldBeReachable: true,
		},
		{
			name:              "UNREACHABLE_www.wikipedia.org_443_over_wikipedia_wildcard_tls",
			config:            "testdata/networking/v1alpha3/wildcard-tls-wikipedia.yaml",
			url:               "https://www.wikipedia.org",
			shouldBeReachable: false,
		},
		{
			name:              "UNREACHABLE_www.bing.com_443_over_wikipedia_wildcard_tls",
			config:            "testdata/networking/v1alpha3/wildcard-tls-wikipedia.yaml",
			url:               "https://www.bing.com",
			shouldBeReachable: false,
		},
		// test TLS protocol without VS, resolution DNS
		// doesn't match specified host *.google.co.uk or *.google.co.in
		{
			name:              "UNREACHABLE_www.google.com_443_no_vs_over_google_wildcard_tls",
			config:            "testdata/networking/v1alpha3/wildcard-tls-google-no-vs.yaml",
			url:               "https://www.google.com",
			shouldBeReachable: false,
		},
		{
			name:              "UNREACHABLE_www.google.com_80_no_vs_over_google_wildcard_tls",
			config:            "testdata/networking/v1alpha3/wildcard-tls-google-no-vs.yaml",
			url:               "http://www.google.com",
			shouldBeReachable: false,
		},
		{
			name:              "UNREACHABLE_www.google.co.uk_80_no_vs_over_google_wildcard_tls",
			config:            "testdata/networking/v1alpha3/wildcard-tls-google-no-vs.yaml",
			url:               "http://www.google.co.uk/",
			shouldBeReachable: false,
		},
		{
			name:              "UNREACHABLE_www.google.co.in_80_no_vs_over_google_wildcard_tls",
			config:            "testdata/networking/v1alpha3/wildcard-tls-google-no-vs.yaml",
			url:               "http://www.google.co.in/",
			shouldBeReachable: false,
		},
		{
			name:              "REACHABLE_www.google.co.uk_443_no_vs_over_google_wildcard_tls",
			config:            "testdata/networking/v1alpha3/wildcard-tls-google-no-vs.yaml",
			url:               "https://www.google.co.uk/",
			shouldBeReachable: true,
		},
		{
			name:              "REACHABLE_www.google.co.in_443_no_vs_over_google_wildcard_tls",
			config:            "testdata/networking/v1alpha3/wildcard-tls-google-no-vs.yaml",
			url:               "https://www.google.co.in/",
			shouldBeReachable: true,
		},
		// test https without VS - related multihosts with resolution DNS
		{
			name:              "REACHABLE_www.google.co.in_443_no_vs_over_google_wildcard_443",
			config:            "testdata/networking/v1alpha3/wildcard-https-google-no-vs.yaml",
			url:               "https://www.google.co.in/",
			shouldBeReachable: true,
		},
		{
			name:              "REACHABLE_www.google.com.uk_443_no_vs_over_google_wildcard_443",
			config:            "testdata/networking/v1alpha3/wildcard-https-google-no-vs.yaml",
			url:               "https://www.google.co.uk/",
			shouldBeReachable: true,
		},
		{
			name:              "UNREACHABLE_www.google.com_443_no_vs_over_google_wildcard_443_https",
			config:            "testdata/networking/v1alpha3/wildcard-https-google-no-vs.yaml",
			url:               "https://www.google.com",
			shouldBeReachable: false,
		},
		// test unrelated multihosts with resolution NONE
		{
			name:              "REACHABLE_www.google.com_443_no_vs_over_multihosts_wildcard_443",
			config:            "testdata/networking/v1alpha3/wildcard-https-multihosts-no-vs.yaml",
			url:               "https://www.google.com",
			shouldBeReachable: true,
		},
		{
			name:              "REACHABLE_www.bing.com_443_no_vs_over_multihosts_wildcard_443",
			config:            "testdata/networking/v1alpha3/wildcard-https-multihosts-no-vs.yaml",
			url:               "https://www.bing.com",
			shouldBeReachable: true,
		},
		{
			name:              "UNREACHABLE_www.google.com_80_no_vs_over_multihosts_wildcard_443",
			config:            "testdata/networking/v1alpha3/wildcard-https-multihosts-no-vs.yaml",
			url:               "http://www.google.com",
			shouldBeReachable: false,
		},
		{
			name:              "UNREACHABLE_www.bing.com_80_no_vs_over_multihosts_wildcard_443",
			config:            "testdata/networking/v1alpha3/wildcard-https-multihosts-no-vs.yaml",
			url:               "http://www.bing.com",
			shouldBeReachable: false,
		},
		{
			name:              "UNREACHABLE_www.wikipedia.org_443_no_vs_over_multihosts_wildcard_443",
			config:            "testdata/networking/v1alpha3/wildcard-https-multihosts-no-vs.yaml",
			url:               "https://www.wikipedia.org",
			shouldBeReachable: false,
		},
		{
			name:              "REACHABLE_cn.bing.com_443_no_vs_over_multihosts_wildcard_443",
			config:            "testdata/networking/v1alpha3/wildcard-https-multihosts-no-vs.yaml",
			url:               "https://cn.bing.com",
			shouldBeReachable: true,
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
						if reachable && !cs.shouldBeReachable {
							return fmt.Errorf("%s is reachable from %s (should be unreachable)", cs.url, src)
						}
						if !reachable && cs.shouldBeReachable {
							log.Errorf("%s is not reachable while it should be reachable from %s", cs.url, src)
							return errAgain
						}

						return nil
					})
				}
			}
		})
	}
}
