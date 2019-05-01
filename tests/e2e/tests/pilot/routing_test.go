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
					runRetriableTest(t, testName, 5, func() error {
						reqURL := fmt.Sprintf("%s://%s/%s", c.scheme, c.dst, c.src)
						resp := ClientRequest(cluster, c.src, reqURL, samples, fmt.Sprintf("--key %s --val %s", c.headerKey, c.headerVal))
						count := make(map[string]int)
						for _, elt := range resp.Version {
							count[elt]++
						}
						log.Infof("request counts %v", count)
						epsilon := 10

						for version, expected := range c.expectedCount {
							if count[version] > expected+epsilon || count[version] < expected-epsilon {
								return fmt.Errorf("expected %v requests (+/-%v) to reach %s => Got %v",
									expected, epsilon, version, count[version])
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
		runRetriableTest(t, "v1alpha3", 5, func() error {
			reqURL := "http://c/a"

			start := time.Now()
			resp := ClientRequest(cluster, "a", reqURL, 1, "--key version --val v2")
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
		runRetriableTest(t, "v1alpha3", 5, func() error {
			targetHost := "b"
			targetPath := "/new/path"

			reqURL := "http://c/a"
			resp := ClientRequest(cluster, "a", reqURL, 1, "--key testredirect --val enabled")
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
			resp := ClientRequest(cluster, "a", reqURL, 1, fmt.Sprintf("--key X-Request-Id --val %d", i))
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
		runRetriableTest(t, "v1alpha3", 5, func() error {
			reqURL := "http://c/a"
			resp := ClientRequest(cluster, "a", reqURL, 1, "--key envoyfilter-test --val foobar123")

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
	deprecatedHeaderRegexp := regexp.MustCompile("(?i)istio-custom-header=user-defined-value")
	deprecatedReqHeaderRegexp := regexp.MustCompile("(?i)istio-custom-req-header=user-defined-value")
	deprecatedReqHeaderRemoveRegexp := regexp.MustCompile("(?i)istio-custom-req-header-remove=to-be-removed")
	deprecatedRespHeaderRegexp := regexp.MustCompile("(?i)ResponseHeader=istio-custom-resp-header:user-defined-value")
	deprecatedRespHeaderRemoveRegexp := regexp.MustCompile("(?i)ResponseHeader=istio-custom-resp-header-remove:to-be-removed")
	deprecatedDestReqHeaderRegexp := regexp.MustCompile("(?i)istio-custom-dest-req-header=user-defined-value")
	deprecatedDestReqHeaderRemoveRegexp := regexp.MustCompile("(?i)istio-custom-dest-req-header-remove=to-be-removed")
	deprecatedDestRespHeaderRegexp := regexp.MustCompile("(?i)ResponseHeader=istio-custom-dest-resp-header:user-defined-value")
	deprecatedDestRespHeaderRemoveRegexp := regexp.MustCompile("(?i)ResponseHeader=istio-custom-dest-resp-header-remove:to-be-removed")

	// the response body contains the request and response headers.
	// appended headers will appear as separate headers with same name
	// examples from body:
	// [1] ResponseHeader=Res-Append:sent-by-server
	// [1 body] Res-Append:sent-by-server
	reqAppendRegexp1 := regexp.MustCompile("(?i) req-append=sent-by-client")
	reqAppendRegexp2 := regexp.MustCompile("(?i) req-append=added-in-request")
	reqSetRegexp := regexp.MustCompile("(?i) req-replace=new-value")
	reqRemoveRegexp := regexp.MustCompile("(?i) req-remove=to-be-removed")
	resAppendRegexp1 := regexp.MustCompile("(?i)=res-append:sent-by-server")
	resAppendRegexp2 := regexp.MustCompile("(?i)=res-append:added-in-response")
	resSetRegexp := regexp.MustCompile("(?i)=res-replace:new-value")
	resRemoveRegexp := regexp.MustCompile("(?i)=res-remove:to-be-removed")
	dstReqAppendRegexp1 := regexp.MustCompile("(?i) dst-req-append=sent-by-client")
	dstReqAppendRegexp2 := regexp.MustCompile("(?i) dst-req-append=added-in-request")
	dstReqSetRegexp := regexp.MustCompile("(?i) dst-req-replace=new-value")
	dstReqRemoveRegexp := regexp.MustCompile("(?i) dst-req-remove=to-be-removed")
	dstResAppendRegexp1 := regexp.MustCompile("(?i)=dst-res-append:sent-by-server")
	dstResAppendRegexp2 := regexp.MustCompile("(?i)=dst-res-append:added-in-response")
	dstResSetRegexp := regexp.MustCompile("(?i)=dst-res-replace:new-value")
	dstResRemoveRegexp := regexp.MustCompile("(?i)=dst-res-remove:to-be-removed")

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
		runRetriableTest(t, "v1alpha3", 5, func() error {
			reqURL := "http://c/a?headers=" +
				"istio-custom-resp-header-remove:to-be-removed," +
				"istio-custom-dest-resp-header-remove:to-be-removed," +
				"res-append:sent-by-server," +
				"res-replace:to-be-replaced," +
				"res-remove:to-be-removed," +
				"dst-res-append:sent-by-server," +
				"dst-res-replace:to-be-replaced," +
				"dst-res-remove:to-be-removed"

			extra := "--headers istio-custom-req-header-remove:to-be-removed," +
				"istio-custom-dest-req-header-remove:to-be-removed," +
				"req-append:sent-by-client," +
				"req-replace:to-be-replaced," +
				"req-remove:to-be-removed," +
				"dst-req-append:sent-by-client," +
				"dst-req-replace:to-be-replaced," +
				"dst-req-remove:to-be-removed"
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
			if err := verifyCount(deprecatedHeaderRegexp, resp.Body, numRequests); err != nil {
				return multierror.Prefix(err, "route deprecated request header count does not have expected distribution")
			}

			// ensure the route-wide request header add works, regardless of service version
			if err := verifyCount(deprecatedReqHeaderRegexp, resp.Body, numRequests); err != nil {
				return multierror.Prefix(err, "route request header count does not have expected distribution")
			}

			// ensure the route-wide request header remove works, regardless of service version
			if err := verifyCount(deprecatedReqHeaderRemoveRegexp, resp.Body, numNoRequests); err != nil {
				return multierror.Prefix(err, "route to remove request header count does not have expected distribution")
			}

			// ensure the route-wide response header add works, regardless of service version
			if err := verifyCount(deprecatedRespHeaderRegexp, resp.Body, numRequests); err != nil {
				return multierror.Prefix(err, "route response header count does not have expected distribution")
			}

			// ensure the route-wide response header remove works, regardless of service version
			if err := verifyCount(deprecatedRespHeaderRemoveRegexp, resp.Body, numNoRequests); err != nil {
				return multierror.Prefix(err, "route to remove response header count does not have expected distribution")
			}

			// verify request header add count
			if err := verifyCount(deprecatedDestReqHeaderRegexp, resp.Body, numV1); err != nil {
				return multierror.Prefix(err, "destination request header count does not have expected distribution")
			}

			// verify request header remove count
			if err := verifyCount(deprecatedDestReqHeaderRemoveRegexp, resp.Body, numV2); err != nil {
				return multierror.Prefix(err, "destination to remove request header count does not have expected distribution")
			}

			// verify response header add count
			if err := verifyCount(deprecatedDestRespHeaderRegexp, resp.Body, numV1); err != nil {
				return multierror.Prefix(err, "destination response header count does not have expected distribution")
			}

			// verify response header remove count
			if err := verifyCount(deprecatedDestRespHeaderRemoveRegexp, resp.Body, numV2); err != nil {
				return multierror.Prefix(err, "destination to remove response header count does not have expected distribution")
			}

			// begin verify header manipulations
			if err1 := verifyCount(reqAppendRegexp1, resp.Body, numRequests); err1 != nil {
				if err2 := verifyCount(reqAppendRegexp2, resp.Body, numRequests); err2 != nil {
					err := multierror.Append(err1, err2)
					return multierror.Prefix(err, "route level append request headers count does not have expected distribution")
				}
			}

			if err := verifyCount(reqSetRegexp, resp.Body, numRequests); err != nil {
				return multierror.Prefix(err, "route level set request headers count does not have expected distribution")
			}

			if err := verifyCount(reqRemoveRegexp, resp.Body, numNoRequests); err != nil {
				return multierror.Prefix(err, "route level remove request request headers count does not have expected distribution")
			}

			if err1 := verifyCount(resAppendRegexp1, resp.Body, numRequests); err1 != nil {
				if err2 := verifyCount(resAppendRegexp2, resp.Body, numRequests); err2 != nil {
					err := multierror.Append(err1, err2)
					return multierror.Prefix(err, "route level append response headers count does not have expected distribution")
				}
			}

			if err := verifyCount(resSetRegexp, resp.Body, numRequests); err != nil {
				return multierror.Prefix(err, "route level set response headers count does not have expected distribution")
			}

			if err := verifyCount(resRemoveRegexp, resp.Body, numNoRequests); err != nil {
				return multierror.Prefix(err, "route level remove response headers count does not have expected distribution")
			}

			if err1 := verifyCount(dstReqAppendRegexp1, resp.Body, numV1); err1 != nil {
				if err2 := verifyCount(dstReqAppendRegexp2, resp.Body, numV1); err2 != nil {
					err := multierror.Append(err1, err2)
					return multierror.Prefix(err, "destination level append request headers count does not have expected distribution")
				}
			}

			if err := verifyCount(dstReqSetRegexp, resp.Body, numV1); err != nil {
				return multierror.Prefix(err, "destination level set request headers count does not have expected distribution")
			}

			if err := verifyCount(dstReqRemoveRegexp, resp.Body, numV2); err != nil {
				return multierror.Prefix(err, "destination level remove request headers count does not have expected distribution")
			}

			if err1 := verifyCount(dstResAppendRegexp1, resp.Body, numV1); err1 != nil {
				if err2 := verifyCount(dstResAppendRegexp2, resp.Body, numV1); err2 != nil {
					err := multierror.Append(err1, err2)
					return multierror.Prefix(err, "destination level append response headers count does not have expected distribution")
				}
			}

			if err := verifyCount(dstResSetRegexp, resp.Body, numV1); err != nil {
				return multierror.Prefix(err, "destination level set response headers count does not have expected distribution")
			}

			if err := verifyCount(dstResRemoveRegexp, resp.Body, numV2); err != nil {
				return multierror.Prefix(err, "destination level remove response headers count does not have expected distribution")
			}

			return nil
		})
	}
}

func TestDestinationRuleExportTo(t *testing.T) {
	var cfgs []*deployableConfig
	applyRuleFunc := func(t *testing.T, ruleYamls map[string][]string) {
		// Delete the previous rule if there was one. No delay on the teardown, since we're going to apply
		// a delay when we push the new config.
		for _, cfg := range cfgs {
			if cfg != nil {
				if err := cfg.TeardownNoDelay(); err != nil {
					t.Fatal(err)
				}
				cfg = nil
			}
		}

		cfgs = make([]*deployableConfig, 0)
		for ns, rules := range ruleYamls {
			// Apply the new rules in the namespace
			cfg := &deployableConfig{
				Namespace:  ns,
				YamlFiles:  rules,
				kubeconfig: tc.Kube.KubeConfig,
			}
			if err := cfg.Setup(); err != nil {
				t.Fatal(err)
			}
			cfgs = append(cfgs, cfg)
		}
	}
	// Upon function exit, delete the active rule.
	defer func() {
		for _, cfg := range cfgs {
			if cfg != nil {
				_ = cfg.Teardown()
			}
		}
	}()

	// Create the namespaces
	// NOTE: Use different namespaces for each test to avoid
	// collision. Namespace deletion takes time. If the other test
	// starts before this namespace is deleted, namespace creation in the other test will fail.
	for _, ns := range []string{"dns1", "dns2"} {
		if err := util.CreateNamespace(ns, tc.Kube.KubeConfig); err != nil {
			t.Errorf("Unable to create namespace %s: %v", ns, err)
		}
		defer func(ns string) {
			if err := util.DeleteNamespace(ns, tc.Kube.KubeConfig); err != nil {
				t.Errorf("Failed to delete namespace %s", ns)
			}
		}(ns)
	}

	cases := []struct {
		testName        string
		rules           map[string][]string
		src             string
		dst             string
		expectedSuccess bool
		onFailure       func()
	}{
		// TODO: this test cannot be enabled until we start running e2e tests in multiple namespaces or
		// in a namespace other than istio-system - which happens to be the config root namespace
		// only public rules work in the config root namespace
		//{
		//	testName:        "private destination rule in same namespace",
		//	rules:           map[string][]string{tc.Kube.Namespace: {"destination-rule-c-private.yaml"}},
		//	src:             "a",
		//	dst:             "c",
		//	expectedSuccess: true,
		//},
		{
			testName:        "private destination rule in different namespaces",
			rules:           map[string][]string{"dns1": {"destination-rule-c-private.yaml"}},
			src:             "a",
			dst:             "c",
			expectedSuccess: false,
		},
		// TODO: These two cases cannot be tested reliably until the EDS issue with
		// destination rules is fixed. https://github.com/istio/istio/issues/10817
		//{
		//	testName:        "private rule in a namespace overrides public rule in another namespace",
		//},
		//{
		//	testName:        "public rule in service's namespace overrides public rule in another namespace",
		//},
	}

	t.Run("v1alpha3", func(t *testing.T) {
		cfgs := &deployableConfig{
			Namespace:  tc.Kube.Namespace,
			YamlFiles:  []string{"testdata/networking/v1alpha3/rule-default-route.yaml"},
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
				for _, yamls := range c.rules {
					for i, yaml := range yamls {
						ruleYaml := fmt.Sprintf("testdata/networking/v1alpha3/%s", yaml)
						yamls[i] = maybeAddTLSForDestinationRule(tc, ruleYaml)
					}
				}
				applyRuleFunc(t, c.rules)
				// Wait a few seconds so that the older proxy listeners get overwritten
				time.Sleep(10 * time.Second)

				for cluster := range tc.Kube.Clusters {
					testName := fmt.Sprintf("%s from %s cluster", c.testName, cluster)
					runRetriableTest(t, testName, 5, func() error {
						reqURL := fmt.Sprintf("http://%s/%s", c.dst, c.src)
						resp := ClientRequest(cluster, c.src, reqURL, 1, "")
						if c.expectedSuccess && !resp.IsHTTPOk() {
							return fmt.Errorf("failed request %s, %v", reqURL, resp.Code)
						}
						if !c.expectedSuccess && resp.IsHTTPOk() {
							return fmt.Errorf("expect failed request %s, but got success", reqURL)
						}
						return nil
					}, c.onFailure)
				}
			}()
		}
	})
}

func TestSidecarScope(t *testing.T) {
	var cfgs []*deployableConfig
	applyRuleFunc := func(t *testing.T, ruleYamls map[string][]string) {
		// Delete the previous rule if there was one. No delay on the teardown, since we're going to apply
		// a delay when we push the new config.
		for _, cfg := range cfgs {
			if cfg != nil {
				if err := cfg.TeardownNoDelay(); err != nil {
					t.Fatal(err)
				}
				cfg = nil
			}
		}

		cfgs = make([]*deployableConfig, 0)
		for ns, rules := range ruleYamls {
			// Apply the new rules in the namespace
			cfg := &deployableConfig{
				Namespace:  ns,
				YamlFiles:  rules,
				kubeconfig: tc.Kube.KubeConfig,
			}
			if err := cfg.Setup(); err != nil {
				t.Fatal(err)
			}
			cfgs = append(cfgs, cfg)
		}
	}
	// Upon function exit, delete the active rule.
	defer func() {
		for _, cfg := range cfgs {
			if cfg != nil {
				_ = cfg.Teardown()
			}
		}
	}()

	rules := make(map[string][]string)
	rules[tc.Kube.Namespace] = []string{"testdata/networking/v1alpha3/sidecar-scope-ns1-ns2.yaml"}
	rules["ns1"] = []string{
		"testdata/networking/v1alpha3/service-entry-http-scope-public.yaml",
		"testdata/networking/v1alpha3/service-entry-http-scope-private.yaml",
		"testdata/networking/v1alpha3/virtualservice-http-scope-public.yaml",
		"testdata/networking/v1alpha3/virtualservice-http-scope-private.yaml",
	}
	rules["ns2"] = []string{"testdata/networking/v1alpha3/service-entry-tcp-scope-public.yaml"}

	// Create the namespaces and install the rules in each namespace
	for _, ns := range []string{"ns1", "ns2"} {
		if err := util.CreateNamespace(ns, tc.Kube.KubeConfig); err != nil {
			t.Errorf("Unable to create namespace %s: %v", ns, err)
		}
		defer func(ns string) {
			if err := util.DeleteNamespace(ns, tc.Kube.KubeConfig); err != nil {
				t.Errorf("Failed to delete namespace %s", ns)
			}
		}(ns)
	}
	applyRuleFunc(t, rules)
	// Wait a few seconds so that the older proxy listeners get overwritten
	time.Sleep(10 * time.Second)

	cases := []struct {
		testName    string
		reqURL      string
		host        string
		expectedHdr *regexp.Regexp
		reachable   bool
		onFailure   func()
	}{
		{
			testName:  "cannot reach services if not imported",
			reqURL:    "http://c/a",
			host:      "c",
			reachable: false,
		},
		{
			testName:    "ns1: http://bookinfo.com:9999 reachable",
			reqURL:      "http://127.255.0.1:9999/a",
			host:        "bookinfo.com:9999",
			expectedHdr: regexp.MustCompile("(?i) scope=public"),
			reachable:   true,
		},
		{
			testName:  "ns1: http://private.com:9999 not reachable",
			reqURL:    "http://127.255.0.1:9999/a",
			host:      "private.com:9999",
			reachable: false,
		},
		{
			testName:  "ns2: service.tcp.com:8888 reachable",
			reqURL:    "http://127.255.255.11:8888/a",
			host:      "tcp.com",
			reachable: true,
		},
		{
			testName:  "ns1: bookinfo.com:9999 reachable via egress TCP listener 7.7.7.7:23145",
			reqURL:    "http://7.7.7.7:23145/a",
			host:      "bookinfo.com:9999",
			reachable: true,
		},
	}

	t.Run("v1alpha3", func(t *testing.T) {
		for _, c := range cases {
			for cluster := range tc.Kube.Clusters {
				testName := fmt.Sprintf("%s from %s cluster", c.testName, cluster)
				runRetriableTest(t, testName, 5, func() error {
					resp := ClientRequest(cluster, "a", c.reqURL, 1, fmt.Sprintf("--key Host --val %s", c.host))
					if c.reachable && !resp.IsHTTPOk() {
						return fmt.Errorf("cannot reach %s, %v", c.reqURL, resp.Code)
					}

					if !c.reachable && resp.IsHTTPOk() {
						return fmt.Errorf("expected request %s to fail, but got success", c.reqURL)
					}

					if c.reachable && c.expectedHdr != nil {
						found := len(c.expectedHdr.FindAllStringSubmatch(resp.Body, -1))
						if found != 1 {
							return fmt.Errorf("public virtualService for %s is not in effect", c.reqURL)
						}
					}
					return nil
				}, c.onFailure)
			}
		}
	})
}
