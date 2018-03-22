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

// Routing tests

package pilot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/log"
	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

type routing struct {
	*tutil.Environment
}

func (t *routing) String() string {
	return "routing-rules"
}

func (t *routing) Setup() error {
	return nil
}

// TODO: test negatives
func (t *routing) Run() error {
	versions := make([]string, 0)
	if t.Config.V1alpha1 {
		versions = append(versions, "v1alpha1")
	}
	if t.Config.V1alpha3 {
		versions = append(versions, "v1alpha3")
	}

	cases := []struct {
		description string
		config      string
		check       func() error
	}{
		{
			// First test default routing
			description: "routing all traffic to c-v1",
			config:      "rule-default-route.yaml.tmpl",
			check: func() error {
				return t.verifyRouting("http", "a", "c", "", "", 100, map[string]int{"v1": 100, "v2": 0}, "default-route")
			},
		},
		{
			description: "routing 75 percent to c-v1, 25 percent to c-v2",
			config:      "rule-weighted-route.yaml.tmpl",
			check: func() error {
				return t.verifyRouting("http", "a", "c", "", "", 100, map[string]int{"v1": 75, "v2": 25}, "")
			},
		},
		{
			description: "routing 100 percent to c-v2 using header",
			config:      "rule-content-route.yaml.tmpl",
			check: func() error {
				return t.verifyRouting("http", "a", "c", "version", "v2", 100, map[string]int{"v1": 0, "v2": 100}, "")
			},
		},
		{
			description: "routing 100 percent to c-v2 using regex header",
			config:      "rule-regex-route.yaml.tmpl",
			check: func() error {
				return t.verifyRouting("http", "a", "c", "foo", "bar", 100, map[string]int{"v1": 0, "v2": 100}, "")
			},
		},
		{
			description: "fault injection",
			config:      "rule-fault-injection.yaml.tmpl",
			check: func() error {
				return t.verifyFaultInjection("a", "c", "version", "v2", time.Second*5, 503)
			},
		},
		{
			description: "redirect injection",
			config:      "rule-redirect-injection.yaml.tmpl",
			check: func() error {
				return t.verifyRedirect("a", "c", "b", "/new/path", "testredirect", "enabled", 200)
			},
		},
		// In case of websockets, the server does not return headers as part of response.
		// After upgrading to websocket connection, it waits for a dummy message from the
		// client over the websocket connection. It then returns all the headers as
		// part of the response message which is then printed out by the client.
		// So the verify checks here are really parsing the output of a websocket message
		// i.e., we are effectively checking websockets beyond just the upgrade.
		{
			description: "routing 100 percent to c-v1 with websocket upgrades",
			config:      "rule-websocket-route.yaml.tmpl",
			check: func() error {
				return t.verifyRouting("ws", "a", "c", "testwebsocket", "enabled", 100, map[string]int{"v1": 100, "v2": 0}, "")
			},
		},
		{
			description: "routing all traffic to c-v1 with appended headers",
			config:      "rule-default-route-append-headers.yaml.tmpl",
			check: func() error {
				return t.verifyRouting("http", "a", "c", "", "", 100, map[string]int{"v1": 100, "v2": 0}, "default-route")
			},
		},
		{
			description: "routing all traffic to c-v1 with CORS policy",
			config:      "rule-default-route-cors-policy.yaml.tmpl",
			check: func() error {
				return t.verifyRouting("http", "a", "c", "", "", 100, map[string]int{"v1": 100, "v2": 0}, "default-route")
			},
		},
		{
			description: "routing all traffic to c with shadow policy",
			config:      "rule-default-route-mirrored.yaml.tmpl",
			check: func() error {
				return t.verifyRouting("http", "a", "c", "", "", 100, map[string]int{"v1": 50, "v2": 50}, "default-route")
			},
		},
	}

	var errs error
	for _, version := range versions {
		if version == "v1alpha3" {
			if err := t.ApplyConfig("v1alpha3/destination-rule-c.yaml.tmpl", nil); err != nil {
				errs = multierror.Append(errs, err)
				continue
			}
		}
		for _, cs := range cases {
			tutil.Tlog("Checking "+version+" routing test", cs.description)
			if err := t.ApplyConfig(version+"/"+cs.config, nil); err != nil {
				return err
			}

			if err := tutil.Repeat(cs.check, 5, time.Second); err != nil {
				log.Infof("Failed the test with %v", err)
				errs = multierror.Append(errs, multierror.Prefix(err, version+" "+cs.description))
			} else {
				log.Info("Success!")
			}
		}
		log.Infof("Cleaning up %s route rules...", version)
		if err := t.DeleteAllConfigs(); err != nil {
			log.Warna(err)
		}
	}
	return errs
}

func (t *routing) Teardown() {
}

func counts(elts []string) map[string]int {
	out := make(map[string]int)
	for _, elt := range elts {
		out[elt] = out[elt] + 1
	}
	return out
}

// verifyRouting verifies if the traffic is split as specified across different deployments in a service
func (t *routing) verifyRouting(scheme, src, dst, headerKey, headerVal string,
	samples int, expectedCount map[string]int, operation string) error {
	url := fmt.Sprintf("%s://%s/%s", scheme, dst, src)
	log.Infof("Making %d requests (%s) from %s...\n", samples, url, src)

	resp := t.ClientRequest(src, url, samples, fmt.Sprintf("-key %s -val %s", headerKey, headerVal))
	count := counts(resp.Version)
	log.Infof("request counts %v", count)
	epsilon := 10

	var errs error
	for version, expected := range expectedCount {
		if count[version] > expected+epsilon || count[version] < expected-epsilon {
			errs = multierror.Append(errs, fmt.Errorf("expected %v requests (+/-%v) to reach %s => Got %v",
				expected, epsilon, version, count[version]))
		}
	}

	if operation != "" {
		if err := t.verifyDecorator(operation); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return errs
}

// verify that the traces were picked up by Zipkin and decorator has been applied
func (t *routing) verifyDecorator(operation string) error {
	if !t.Config.Zipkin {
		return nil
	}

	response := t.Environment.ClientRequest(
		"t",
		fmt.Sprintf("http://zipkin.%s:9411/api/v1/traces", t.Config.IstioNamespace),
		1, "",
	)

	if !response.IsHTTPOk() {
		return fmt.Errorf("could not retrieve traces from zipkin")
	}

	text := fmt.Sprintf("\"name\":\"%s\"", operation)
	if strings.Count(response.Body, text) != 10 {
		return fmt.Errorf("could not find operation %q in zipkin traces", operation)
	}

	return nil
}

// verifyFaultInjection verifies if the fault filter was setup properly
func (t *routing) verifyFaultInjection(src, dst, headerKey, headerVal string,
	respTime time.Duration, respCode int) error {
	url := fmt.Sprintf("http://%s/%s", dst, src)
	log.Infof("Making 1 request (%s) from %s...\n", url, src)

	start := time.Now()
	resp := t.ClientRequest(src, url, 1, fmt.Sprintf("-key %s -val %s", headerKey, headerVal))
	elapsed := time.Since(start)

	statusCode := ""
	if len(resp.Code) > 0 {
		statusCode = resp.Code[0]
	}

	// +/- 2s variance
	epsilon := time.Second * 2
	if elapsed > respTime+epsilon || elapsed < respTime-epsilon || strconv.Itoa(respCode) != statusCode {
		return fmt.Errorf("fault injection verification failed: "+
			"response time is %s with status code %s, "+
			"expected response time is %s +/- %s with status code %d", elapsed, statusCode, respTime, epsilon, respCode)
	}
	return nil
}

// verifyRedirect verifies if the http redirect was setup properly
func (t *routing) verifyRedirect(src, dst, targetHost, targetPath, headerKey, headerVal string, respCode int) error {
	url := fmt.Sprintf("http://%s/%s", dst, src)
	log.Infof("Making 1 request (%s) from %s...\n", url, src)

	failureMsg := "redirect verification failed"
	resp := t.ClientRequest(src, url, 1, fmt.Sprintf("-key %s -val %s", headerKey, headerVal))
	if len(resp.Code) == 0 || resp.Code[0] != fmt.Sprint(respCode) {
		return fmt.Errorf("%s: response status code: %v, expected %v", failureMsg, resp.Code, respCode)
	}

	var host string
	if matches := regexp.MustCompile("(?i)Host=(.*)").FindStringSubmatch(resp.Body); len(matches) >= 2 {
		host = matches[1]
	}
	if host != targetHost {
		return fmt.Errorf("%s: response body contains Host=%v, expected Host=%v", failureMsg, host, targetHost)
	}

	exp := regexp.MustCompile("(?i)URL=(.*)")
	paths := exp.FindAllStringSubmatch(resp.Body, -1)
	var path string
	if len(paths) > 1 {
		path = paths[1][1]
	}
	if path != targetPath {
		return fmt.Errorf("%s: response body contains URL=%v, expected URL=%v", failureMsg, path, targetPath)
	}

	return nil
}
