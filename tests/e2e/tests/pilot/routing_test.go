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
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/util"
)

func TestRoutes(t *testing.T) {
	samples := 100

	var cfgs *deployableConfig
	applyRuleFunc := func(t *testing.T, ruleYaml string) {
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
	// Upon function exit, delete the active rule.
	defer func() {
		if cfgs != nil {
			_ = cfgs.Teardown()
		}
	}()

	cases := []struct {
		testName      string
		description   string
		config        string
		scheme        string
		src           string
		dst           string
		headerKey     string
		headerVal     string
		expectedCount map[string]int
		operation     string
		onFailure     func()
	}{
		{
			// First test default routing
			testName:      "a->c[v1=100]",
			description:   "routing all traffic to c-v1",
			config:        "rule-default-route.yaml",
			scheme:        "http",
			src:           "a",
			dst:           "c",
			headerKey:     "",
			headerVal:     "",
			expectedCount: map[string]int{"v1": 100, "v2": 0},
			operation:     "c.istio-system.svc.cluster.local:80/*",
		},
		{
			testName:      "a->c[v1=75,v2=25]",
			description:   "routing 75 percent to c-v1, 25 percent to c-v2",
			config:        "rule-weighted-route.yaml",
			scheme:        "http",
			src:           "a",
			dst:           "c",
			headerKey:     "",
			headerVal:     "",
			expectedCount: map[string]int{"v1": 75, "v2": 25},
		},
		{
			testName:      "a->c[v2=100]_header",
			description:   "routing 100 percent to c-v2 using header",
			config:        "rule-content-route.yaml",
			scheme:        "http",
			src:           "a",
			dst:           "c",
			headerKey:     "version",
			headerVal:     "v2",
			expectedCount: map[string]int{"v1": 0, "v2": 100},
			operation:     "",
		},
		{
			testName:      "a->c[v2=100]_regex_header",
			description:   "routing 100 percent to c-v2 using regex header",
			config:        "rule-regex-route.yaml",
			scheme:        "http",
			src:           "a",
			dst:           "c",
			headerKey:     "foo",
			headerVal:     "bar",
			expectedCount: map[string]int{"v1": 0, "v2": 100},
			operation:     "",
			onFailure: func() {
				op, err := tc.Kube.GetRoutes("a")
				log.Infof("error: %v\n%s", err, op)
				cfg, err := util.GetConfigs("destinationrules.networking.istio.io",
					"virtualservices.networking.istio.io", "serviceentries.networking.istio.io",
					"policies.authentication.istio.io")

				log.Infof("config: %v\n%s", err, cfg)
			},
		},
		// In case of websockets, the server does not return headers as part of response.
		// After upgrading to websocket connection, it waits for a dummy message from the
		// client over the websocket connection. It then returns all the headers as
		// part of the response message which is then printed out by the client.
		// So the verify checks here are really parsing the output of a websocket message
		// i.e., we are effectively checking websockets beyond just the upgrade.
		{
			testName:      "a->c[v1=100]_websocket",
			description:   "routing 100 percent to c-v1 with websocket upgrades",
			config:        "rule-websocket-route.yaml",
			scheme:        "ws",
			src:           "a",
			dst:           "c",
			headerKey:     "testwebsocket",
			headerVal:     "enabled",
			expectedCount: map[string]int{"v1": 100, "v2": 0},
			operation:     "",
		},
		{
			testName:      "a->c[v1=100]_append_headers",
			description:   "routing all traffic to c-v1 with appended headers",
			config:        "rule-default-route-append-headers.yaml",
			scheme:        "http",
			src:           "a",
			dst:           "c",
			headerKey:     "",
			headerVal:     "",
			expectedCount: map[string]int{"v1": 75, "v2": 25},
			operation:     "c.istio-system.svc.cluster.local:80/*",
		},
		{
			testName:      "a->c[v1=100]_CORS_policy",
			description:   "routing all traffic to c-v1 with CORS policy",
			config:        "rule-default-route-cors-policy.yaml",
			scheme:        "http",
			src:           "a",
			dst:           "c",
			headerKey:     "",
			headerVal:     "",
			expectedCount: map[string]int{"v1": 100, "v2": 0},
			operation:     "c.istio-system.svc.cluster.local:80/*",
		},
		{
			testName:      "a->c[v2=100]",
			description:   "routing tcp traffic from a to c-v2",
			config:        "virtualservice-route-tcp-a.yaml",
			scheme:        "http",
			src:           "a",
			dst:           "c:90",
			headerKey:     "",
			headerVal:     "",
			expectedCount: map[string]int{"v1": 0, "v2": 100},
			operation:     "",
		},
		{
			testName:      "b->c[v1=100]",
			description:   "routing tcp traffic from b to c-v1",
			config:        "virtualservice-route-tcp-a.yaml",
			scheme:        "http",
			src:           "b",
			dst:           "c:90",
			headerKey:     "",
			headerVal:     "",
			expectedCount: map[string]int{"v1": 100, "v2": 0},
			operation:     "",
		},
		{
			testName:      "a->c[v2=100]_tcp_single",
			description:   "routing tcp traffic from a to single dest c-v2",
			config:        "virtualservice-route-tcp-weighted.yaml",
			scheme:        "http",
			src:           "a",
			dst:           "c:90",
			headerKey:     "",
			headerVal:     "",
			expectedCount: map[string]int{"v1": 0, "v2": 100},
			operation:     "",
		},
		{
			testName:      "b->c[v1=30,v2=70]_tcp_weighted",
			description:   "routing 30 percent to c-v1, 70 percent to c-v2",
			config:        "virtualservice-route-tcp-weighted.yaml",
			scheme:        "http",
			src:           "b",
			dst:           "c:90",
			headerKey:     "",
			headerVal:     "",
			expectedCount: map[string]int{"v1": 30, "v2": 70},
			operation:     "",
		},
	}

	t.Run("v1alpha3", func(t *testing.T) {
		destRule1 := maybeAddTLSForDestinationRule(tc, "testdata/networking/v1alpha3/destination-rule-c.yaml")
		destRule2 := "testdata/networking/v1alpha3/destination-rule-c-headersubset.yaml"
		cfgs := &deployableConfig{
			Namespace:  tc.Kube.Namespace,
			YamlFiles:  []string{destRule1, destRule2},
			kubeconfig: tc.Kube.KubeConfig,
		}
		if err := cfgs.Setup(); err != nil {
			t.Fatal(err)
		}
		// Teardown after, but no need to wait, since a delay will be applied by either the next rule's
		// Setup() or the Teardown() for the final rule.
		defer cfgs.TeardownNoDelay()

		for _, c := range cases {
			// Run each case in a function to scope the configuration's lifecycle.
			func() {
				ruleYaml := fmt.Sprintf("testdata/networking/v1alpha3/%s", c.config)
				applyRuleFunc(t, ruleYaml)

				for cluster := range tc.Kube.Clusters {
					testName := fmt.Sprintf("%s from %s cluster", c.testName, cluster)
					runRetriableTest(t, cluster, testName, 5, func() error {
						reqURL := fmt.Sprintf("%s://%s/%s", c.scheme, c.dst, c.src)
						resp := ClientRequest(cluster, c.src, reqURL, samples, fmt.Sprintf("-key %s -val %s", c.headerKey, c.headerVal))
						count := make(map[string]int)
						for _, elt := range resp.Version {
							count[elt] = count[elt] + 1
						}
						log.Infof("request counts %v", count)
						epsilon := 10

						for version, expected := range c.expectedCount {
							if count[version] > expected+epsilon || count[version] < expected-epsilon {
								return fmt.Errorf("expected %v requests (+/-%v) to reach %s => Got %v",
									expected, epsilon, version, count[version])
							}
						}

						// Only test this on the primary cluster since zipkin is not available on the remote
						if c.operation != "" && cluster == primaryCluster {
							response := ClientRequest(
								cluster,
								"t",
								fmt.Sprintf("http://zipkin.%s:9411/api/v1/traces", tc.Kube.Namespace),
								1, "",
							)

							if !response.IsHTTPOk() {
								return fmt.Errorf("could not retrieve traces from zipkin")
							}

							text := fmt.Sprintf("\"name\":\"%s\"", c.operation)
							if strings.Count(response.Body, text) != 10 {
								t.Logf("could not find operation %q in zipkin traces: %v", c.operation, response.Body)
							}
						}

						return nil
					}, c.onFailure)
				}
			}()
		}
	})
}

func TestRouteFaultInjection(t *testing.T) {
	destRule := maybeAddTLSForDestinationRule(tc, "testdata/networking/v1alpha3/destination-rule-c.yaml")
	dRule := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{destRule},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := dRule.Setup(); err != nil {
		t.Fatal(err)
	}
	// Teardown after, but no need to wait, since a delay will be applied by either the next rule's
	// Setup() or the Teardown() for the final rule.
	defer dRule.TeardownNoDelay()

	ruleYaml := fmt.Sprintf("testdata/networking/v1alpha3/rule-fault-injection.yaml")
	cfgs := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{ruleYaml},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	for cluster := range tc.Kube.Clusters {
		runRetriableTest(t, cluster, "v1alpha3", 5, func() error {
			reqURL := "http://c/a"

			start := time.Now()
			resp := ClientRequest(cluster, "a", reqURL, 1, "-key version -val v2")
			elapsed := time.Since(start)

			statusCode := ""
			if len(resp.Code) > 0 {
				statusCode = resp.Code[0]
			}

			respCode := 503
			respTime := time.Second * 5
			epsilon := time.Second * 2 // +/- 2s variance
			if elapsed > respTime+epsilon || elapsed < respTime-epsilon || strconv.Itoa(respCode) != statusCode {
				return fmt.Errorf("fault injection verification failed: "+
					"response time is %s with status code %s, "+
					"expected response time is %s +/- %s with status code %d", elapsed, statusCode, respTime, epsilon, respCode)
			}
			return nil
		})
	}
}

func TestRouteRedirectInjection(t *testing.T) {
	// Push the rule config.
	ruleYaml := fmt.Sprintf("testdata/networking/v1alpha3/rule-redirect-injection.yaml")
	cfgs := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{ruleYaml},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	for cluster := range tc.Kube.Clusters {
		runRetriableTest(t, cluster, "v1alpha3", 5, func() error {
			targetHost := "b"
			targetPath := "/new/path"

			reqURL := "http://c/a"
			resp := ClientRequest(cluster, "a", reqURL, 1, "-key testredirect -val enabled")
			if !resp.IsHTTPOk() {
				return fmt.Errorf("redirect failed: response status code: %v, expected 200", resp.Code)
			}

			var host string
			if matches := regexp.MustCompile("(?i)Host=(.*)").FindStringSubmatch(resp.Body); len(matches) >= 2 {
				host = matches[1]
			}
			if host != targetHost {
				return fmt.Errorf("redirect failed: response body contains Host=%v, expected Host=%v", host, targetHost)
			}

			exp := regexp.MustCompile("(?i)URL=(.*)")
			paths := exp.FindAllStringSubmatch(resp.Body, -1)
			var path string
			if len(paths) > 1 {
				path = paths[1][1]
			}
			if path != targetPath {
				return fmt.Errorf("redirect failed: response body contains URL=%v, expected URL=%v", path, targetPath)
			}

			return nil
		})
	}
}

func TestRouteMirroring(t *testing.T) {
	logs := newAccessLogs()
	cfgs := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{"testdata/networking/v1alpha3/rule-default-route-mirrored.yaml"},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	reqURL := "http://c/a"
	for cluster := range tc.Kube.Clusters {
		for i := 1; i <= 100; i++ {
			resp := ClientRequest(cluster, "a", reqURL, 1, fmt.Sprintf("-key X-Request-Id -val %d", i))
			logEntry := fmt.Sprintf("HTTP request from a in %s cluster to c.istio-system.svc.cluster.local:80", cluster)
			if len(resp.ID) > 0 {
				id := resp.ID[0]
				logs.add(cluster, "c", id, logEntry)
				logs.add(cluster, "b", id, logEntry) // Request should also be mirrored here
			}
		}
	}

	t.Run("check", func(t *testing.T) {
		logs.checkLogs(t)
	})
}

// Inject a fault filter in a normal path and check if the filters are triggered
func TestEnvoyFilterConfigViaCRD(t *testing.T) {
	cfgs := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{"testdata/networking/v1alpha3/envoyfilter-c.yaml"},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	for cluster := range tc.Kube.Clusters {
		runRetriableTest(t, cluster, "v1alpha3", 5, func() error {
			reqURL := "http://c/a"
			resp := ClientRequest(cluster, "a", reqURL, 1, "-key envoyfilter-test -val foobar123")

			statusCode := ""
			if len(resp.Code) > 0 {
				statusCode = resp.Code[0]
			}

			expectedRespCode := 444
			if strconv.Itoa(expectedRespCode) != statusCode {
				return fmt.Errorf("test configuration of envoy filters via CRD (EnvoyFilter) failed."+
					"Expected %d response code from the manually configured fault filter. Got %s", expectedRespCode, statusCode)
			}
			return nil
		})
	}
}

func TestHeadersManipulations(t *testing.T) {
	destRule := maybeAddTLSForDestinationRule(tc, "testdata/networking/v1alpha3/destination-rule-c.yaml")
	dRule := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{destRule},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := dRule.Setup(); err != nil {
		t.Fatal(err)
	}
	// Teardown after, but no need to wait, since a delay will be applied by either the next rule's
	// Setup() or the Teardown() for the final rule.
	defer dRule.TeardownNoDelay()

	ruleYaml := fmt.Sprintf("testdata/networking/v1alpha3/rule-default-route-append-headers.yaml")
	cfgs := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{ruleYaml},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	// deprecated
	deprecatedReqHeaderRegexp := regexp.MustCompile("(?i)istio-custom-header=user-defined-value")

	reqHeaderRegexp := regexp.MustCompile("(?i)istio-custom-req-header=user-defined-value")
	reqHeaderRemoveRegexp := regexp.MustCompile("(?i)istio-custom-req-header-remove=to-be-removed")
	respHeaderRegexp := regexp.MustCompile("(?i)ResponseHeader=istio-custom-resp-header:user-defined-value")
	respHeaderRemoveRegexp := regexp.MustCompile("(?i)ResponseHeader=istio-custom-resp-header-remove:to-be-removed")

	destReqHeaderRegexp := regexp.MustCompile("(?i)istio-custom-dest-req-header=user-defined-value")
	destReqHeaderRemoveRegexp := regexp.MustCompile("(?i)istio-custom-dest-req-header-remove=to-be-removed")
	destRespHeaderRegexp := regexp.MustCompile("(?i)ResponseHeader=istio-custom-dest-resp-header:user-defined-value")
	destRespHeaderRemoveRegexp := regexp.MustCompile("(?i)ResponseHeader=istio-custom-dest-resp-header-remove:to-be-removed")

	epsilon := 10
	numRequests := 100
	numNoRequests := 0
	numV1 := 75
	numV2 := 25

	verifyCount := func(exp *regexp.Regexp, body string, expected int) error {
		found := len(exp.FindAllStringSubmatch(body, -1))
		if found < expected-epsilon || found > expected+epsilon {
			return fmt.Errorf("got %d, expected %d +/- %d", found, expected, epsilon)
		}
		return nil
	}

	// traffic is split 75/25 between v1 and v2
	// traffic to v1 has header manipulation rules, v2 does not
	// verify the split, and then verify the header counts match what we would expect
	//
	// note: we do not explicitly verify that individual requests all have the proper
	// header manipulation performed on them. this isn't ideal, but that is difficult
	// to verify this without either,
	// a) making each request individually (very slow)
	// b) or adding a bunch more parsing to the request function, so we have access to headers per request.
	for cluster := range tc.Kube.Clusters {
		runRetriableTest(t, cluster, "v1alpha3", 5, func() error {
			reqURL := "http://c/a?headers=istio-custom-resp-header-remove:to-be-removed,istio-custom-dest-resp-header-remove:to-be-removed"

			extra := "-headers istio-custom-req-header-remove:to-be-removed," +
				"istio-custom-dest-req-header-remove:to-be-removed"
			resp := ClientRequest(cluster, "a", reqURL, numRequests, extra)

			// ensure version distribution
			counts := make(map[string]int)
			for _, version := range resp.Version {
				counts[version]++
			}
			if counts["v1"] < numV1-epsilon || counts["v1"] > numV1+epsilon {
				return fmt.Errorf("expected %d +/- %d requests to reach v1, got %d", numV1, epsilon, counts["v1"])
			}
			if counts["v2"] < numV2-epsilon || counts["v2"] > numV2+epsilon {
				return fmt.Errorf("expected %d +/- %d requests to reach v2, got %d", numV2, epsilon, counts["v2"])
			}

			// ensure the route-wide deprecated request header add works, regardless of service version
			if err := verifyCount(deprecatedReqHeaderRegexp, resp.Body, numRequests); err != nil {
				return multierror.Prefix(err, "route deprecated request header count does not have expected distribution")
			}

			// ensure the route-wide request header add works, regardless of service version
			if err := verifyCount(reqHeaderRegexp, resp.Body, numRequests); err != nil {
				return multierror.Prefix(err, "route request header count does not have expected distribution")
			}

			// ensure the route-wide request header remove works, regardless of service version
			if err := verifyCount(reqHeaderRemoveRegexp, resp.Body, numNoRequests); err != nil {
				return multierror.Prefix(err, "route to remove request header count does not have expected distribution")
			}

			// ensure the route-wide response header add works, regardless of service version
			if err := verifyCount(respHeaderRegexp, resp.Body, numRequests); err != nil {
				return multierror.Prefix(err, "route response header count does not have expected distribution")
			}

			// ensure the route-wide response header remove works, regardless of service version
			if err := verifyCount(respHeaderRemoveRegexp, resp.Body, numNoRequests); err != nil {
				return multierror.Prefix(err, "route to remove response header count does not have expected distribution")
			}

			// verify request header add count
			if err := verifyCount(destReqHeaderRegexp, resp.Body, numV1); err != nil {
				return multierror.Prefix(err, "destination request header count does not have expected distribution")
			}

			// verify request header remove count
			if err := verifyCount(destReqHeaderRemoveRegexp, resp.Body, numV2); err != nil {
				return multierror.Prefix(err, "destination to remove request header count does not have expected distribution")
			}

			// verify response header add count
			if err := verifyCount(destRespHeaderRegexp, resp.Body, numV1); err != nil {
				return multierror.Prefix(err, "destination response header count does not have expected distribution")
			}

			// verify response header remove count
			if err := verifyCount(destRespHeaderRemoveRegexp, resp.Body, numV2); err != nil {
				return multierror.Prefix(err, "destination to remove response header count does not have expected distribution")
			}

			return nil
		})
	}
}
