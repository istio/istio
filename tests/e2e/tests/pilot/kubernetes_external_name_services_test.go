// Copyright 2017,2018 Istio Authors
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

func TestRewriteExternalService(t *testing.T) {
	// external name service tests are still using v1alpha1 route rules.
	// TODO: need to migrate
	if !tc.Egress {
		t.Skipf("Skipping %s: egress=false", t.Name())
	}
	// egress rules are v1alpha1
	if !tc.V1alpha1 {
		t.Skipf("Skipping %s: v1alpha1=false", t.Name())
	}

	// Apply the rule
	cfgs := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{"testdata/v1alpha1/rule-rewrite-authority-externalbin.yaml"},
	}

	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	srcPods := []struct {
		pod            string
		withIstioProxy bool
	}{
		{
			pod:            "a",
			withIstioProxy: true,
		},
		{
			pod:            "b",
			withIstioProxy: true,
		},
		{
			pod:            "t",
			withIstioProxy: false,
		},
	}

	dstServices := []struct {
		service      string
		externalHost string
	}{
		{
			service:      "externalwikipedia",
			externalHost: "wikipedia.org",
		},
		{
			service:      "externalbin",
			externalHost: "httpbin.org",
		},
	}

	for _, src := range srcPods {
		for _, dst := range dstServices {
			for _, domain := range []string{"", "." + tc.Kube.Namespace} {
				reqURL := fmt.Sprintf("http://%s%s", dst.service, domain)
				extra := ""
				if !src.withIstioProxy {
					extra = "-key Host -val " + dst.externalHost
				}

				testName := fmt.Sprintf("%s->%s%s", src.pod, dst.service, domain)
				runRetriableTest(t, testName, defaultRetryBudget, func() error {
					resp := ClientRequest(src.pod, reqURL, 1, extra)
					if resp.IsHTTPOk() {
						return nil
					}
					return errAgain
				})
			}
		}
	}
}
