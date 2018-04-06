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

	"istio.io/istio/pkg/log"
)

func TestIngress(t *testing.T) {
	if !tc.Ingress || !tc.V1alpha3 {
		t.Skipf("Skipping %s: ingress=false", t.Name())
	}

	istioNamespace := tc.Kube.IstioSystemNamespace()
	ingressServiceName := tc.Kube.IstioIngressService()

	// Configure a route for "c" only
	cfgs := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{
			"testdata/ingress.yaml",
			"testdata/v1alpha1/rule-default-route.yaml"},
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	if !tc.V1alpha3 {
		// First, verify that version splitting is applied to ingress paths.
		runRetriableTest(t, "VersionSplitting", defaultRetryBudget, func() error {
			reqURL := fmt.Sprintf("http://%s.%s/c", ingressServiceName, istioNamespace)
			resp := ClientRequest("t", reqURL, 100, "")
			count := make(map[string]int)
			for _, elt := range resp.Version {
				count[elt] = count[elt] + 1
			}
			log.Infof("request counts %v", count)
			if count["v1"] >= 95 {
				return nil
			}
			return errAgain
		})
	}

	cases := []struct {
		// empty destination to expect 404
		dst  string
		url  string
		host string
	}{
		{
			dst:  "a",
			url:  fmt.Sprintf("https://%s.%s:443/http", ingressServiceName, istioNamespace),
			host: "",
		},
		{
			dst:  "b",
			url:  fmt.Sprintf("https://%s.%s:443/pasta", ingressServiceName, istioNamespace),
			host: "",
		},
		{
			dst:  "a",
			url:  fmt.Sprintf("http://%s.%s/lucky", ingressServiceName, istioNamespace),
			host: "",
		},
		{
			dst:  "a",
			url:  fmt.Sprintf("http://%s.%s/.well_known/foo", ingressServiceName, istioNamespace),
			host: "",
		},
		{
			dst:  "a",
			url:  fmt.Sprintf("http://%s.%s/io.grpc/method", ingressServiceName, istioNamespace),
			host: "",
		},
		{
			dst:  "b",
			url:  fmt.Sprintf("http://%s.%s/lol", ingressServiceName, istioNamespace),
			host: "",
		},
		{
			dst:  "a",
			url:  fmt.Sprintf("http://%s.%s/foo", ingressServiceName, istioNamespace),
			host: "foo.bar.com",
		},
		{
			dst:  "a",
			url:  fmt.Sprintf("http://%s.%s/bar", ingressServiceName, istioNamespace),
			host: "foo.baz.com",
		},
		{
			dst:  "a",
			url:  fmt.Sprintf("grpc://%s.%s:80", ingressServiceName, istioNamespace),
			host: "api.company.com",
		},
		{
			dst:  "a",
			url:  fmt.Sprintf("grpcs://%s.%s:443", ingressServiceName, istioNamespace),
			host: "api.company.com",
		},
		{
			// Expect 404: no match for path
			dst:  "",
			url:  fmt.Sprintf("http://%s.%s/notfound", ingressServiceName, istioNamespace),
			host: "",
		},
		{
			// Expect 404: wrong service port for path
			dst:  "",
			url:  fmt.Sprintf("http://%s.%s/foo", ingressServiceName, istioNamespace),
			host: "",
		},
	}

	logs := newAccessLogs()
	t.Run("request", func(t *testing.T) {
		for _, c := range cases {
			extra := ""
			if c.host != "" {
				extra = "-key Host -val " + c.host
			}

			expectReachable := c.dst != ""
			retryBudget := defaultRetryBudget
			testName := fmt.Sprintf("REACHABLE:%s(Host:%s)", c.url, c.host)
			if !expectReachable {
				retryBudget = 5
				testName = fmt.Sprintf("UNREACHABLE:%s(Host:%s)", c.url, c.host)
			}

			logEntry := fmt.Sprintf("Ingress request to %+v", c)
			runRetriableTest(t, testName, retryBudget, func() error {
				resp := ClientRequest("t", c.url, 1, extra)
				if !expectReachable {
					if len(resp.Code) > 0 && resp.Code[0] == "404" {
						return nil
					}
					return errAgain
				}
				if len(resp.ID) > 0 {
					if !strings.Contains(resp.Body, "X-Forwarded-For") &&
						!strings.Contains(resp.Body, "x-forwarded-for") {
						log.Warnf("Missing X-Forwarded-For in the body: %s", resp.Body)
						return errAgain
					}

					id := resp.ID[0]
					logs.add(c.dst, id, logEntry)
					logs.add(ingressAppName, id, logEntry)
					return nil
				}
				return errAgain
			})
		}
	})

	// After all requests complete, run the check logs tests.
	// Run all request tests in parallel.
	t.Run("check", func(t *testing.T) {
		logs.checkLogs(t)
	})
}
