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

package main

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
)

type routing struct {
	*infra
}

func (t *routing) String() string {
	return "routing-rules"
}

func (t *routing) setup() error {
	return nil
}

// TODO: test negatives
func (t *routing) run() error {
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
				return t.verifyRouting("http", "a", "c", "", "", 100, map[string]int{"v1": 100, "v2": 0})
			},
		},
		{
			description: "routing 75 percent to c-v1, 25 percent to c-v2",
			config:      "rule-weighted-route.yaml.tmpl",
			check: func() error {
				return t.verifyRouting("http", "a", "c", "", "", 100, map[string]int{"v1": 75, "v2": 25})
			},
		},
		{
			description: "routing 100 percent to c-v2 using header",
			config:      "rule-content-route.yaml.tmpl",
			check: func() error {
				return t.verifyRouting("http", "a", "c", "version", "v2", 100, map[string]int{"v1": 0, "v2": 100})
			},
		},
		{
			description: "routing 100 percent to c-v2 using regex header",
			config:      "rule-regex-route.yaml.tmpl",
			check: func() error {
				return t.verifyRouting("http", "a", "c", "foo", "bar", 100, map[string]int{"v1": 0, "v2": 100})
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
				return t.verifyRouting("ws", "a", "c", "testwebsocket", "enabled", 100, map[string]int{"v1": 100, "v2": 0})
			},
		},
	}

	var errs error
	for _, cs := range cases {
		log("Checking routing test", cs.description)
		if err := t.applyConfig(cs.config, nil); err != nil {
			return err
		}

		if err := repeat(cs.check, 3, time.Second); err != nil {
			glog.Infof("Failed the test with %v", err)
			errs = multierror.Append(errs, multierror.Prefix(err, cs.description))
		} else {
			glog.Info("Success!")
		}
	}
	return errs
}

func (t *routing) teardown() {
	glog.Info("Cleaning up route rules...")
	if err := t.deleteAllConfigs(); err != nil {
		glog.Warning(err)
	}
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
	samples int, expectedCount map[string]int) error {
	url := fmt.Sprintf("%s://%s/%s", scheme, dst, src)
	glog.Infof("Making %d requests (%s) from %s...\n", samples, url, src)

	resp := t.clientRequest(src, url, samples, fmt.Sprintf("-key %s -val %s", headerKey, headerVal))
	count := counts(resp.version)
	glog.Infof("request counts %v", count)
	epsilon := 5

	var errs error
	for version, expected := range expectedCount {
		if count[version] > expected+epsilon || count[version] < expected-epsilon {
			errs = multierror.Append(errs, fmt.Errorf("expected %v requests (+/-%v) to reach %s => Got %v",
				expected, epsilon, version, count[version]))
		}
	}

	return errs
}

// verifyFaultInjection verifies if the fault filter was setup properly
func (t *routing) verifyFaultInjection(src, dst, headerKey, headerVal string,
	respTime time.Duration, respCode int) error {
	url := fmt.Sprintf("http://%s/%s", dst, src)
	glog.Infof("Making 1 request (%s) from %s...\n", url, src)

	start := time.Now()
	resp := t.clientRequest(src, url, 1, fmt.Sprintf("-key %s -val %s", headerKey, headerVal))
	elapsed := time.Since(start)

	statusCode := ""
	if len(resp.code) > 0 {
		statusCode = resp.code[0]
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
	glog.Infof("Making 1 request (%s) from %s...\n", url, src)

	resp := t.clientRequest(src, url, 1, fmt.Sprintf("-key %s -val %s", headerKey, headerVal))
	if len(resp.code) == 0 || resp.code[0] != fmt.Sprint(respCode) {
		return fmt.Errorf("redirect verification failed: "+
			"response status code: %v, expected %v",
			resp.code, respCode)
	}

	var host string
	if matches := regexp.MustCompile("(?i)Host=(.*)").FindStringSubmatch(resp.body); len(matches) >= 2 {
		host = matches[1]
	}
	if host != targetHost {
		return fmt.Errorf("redirect verification failed: "+
			"response body contains Host=%v, expected Host=%v",
			host, targetHost)
	}

	exp := regexp.MustCompile("(?i)URL=(.*)")
	paths := exp.FindAllStringSubmatch(resp.body, -1)
	var path string
	if len(paths) > 1 {
		path = paths[1][1]
	}
	if path != targetPath {
		return fmt.Errorf("redirect verification failed: "+
			"response body contains URL=%v, expected URL=%v",
			path, targetPath)
	}

	return nil
}
