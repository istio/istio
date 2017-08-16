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
	"istio.io/pilot/model"
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
	// First test default routing
	glog.Info("Routing all traffic to c-v1 and verifying...")
	if err := t.applyConfig("rule-default-route.yaml.tmpl", map[string]string{
		"Destination": "c",
		"Namespace":   t.Namespace,
	}, model.RouteRule.Type); err != nil {
		return err
	}
	if err := t.verifyRouting("a", "c", "", "",
		100, map[string]int{
			"v1": 100,
			"v2": 0,
		}); err != nil {
		return err
	}
	glog.Info("Success!")

	glog.Info("Routing 75 percent to c-v1, 25 percent to c-v2 and verifying...")
	if err := t.applyConfig("rule-weighted-route.yaml.tmpl", map[string]string{
		"Destination": "c",
		"Namespace":   t.Namespace,
	}, model.RouteRule.Type); err != nil {
		return err
	}
	if err := t.verifyRouting("a", "c", "", "",
		100, map[string]int{
			"v1": 75,
			"v2": 25,
		}); err != nil {
		return err
	}
	glog.Info("Success!")

	glog.Info("Routing 100 percent to c-v2 using header based routing and verifying...")
	if err := t.applyConfig("rule-content-route.yaml.tmpl", map[string]string{
		"Source":      "a",
		"Destination": "c",
		"Namespace":   t.Namespace,
	}, model.RouteRule.Type); err != nil {
		return err
	}
	if err := t.verifyRouting("a", "c", "version", "v2",
		100, map[string]int{
			"v1": 0,
			"v2": 100,
		}); err != nil {
		return err
	}
	glog.Info("Success!")

	glog.Info("Routing 100 percent to c-v2 using regex header based routing and verifying...")
	if err := t.applyConfig("rule-regex-route.yaml.tmpl", map[string]string{
		"Source":      "a",
		"Destination": "c",
		"Namespace":   t.Namespace,
	}, model.RouteRule.Type); err != nil {
		return err
	}
	if err := t.verifyRouting("a", "c", "foo", "bar",
		100, map[string]int{
			"v1": 0,
			"v2": 100,
		}); err != nil {
		return err
	}
	glog.Info("Success!")

	glog.Info("Testing fault injection..")
	if err := t.applyConfig("rule-fault-injection.yaml.tmpl", map[string]string{
		"Source":      "a",
		"Destination": "c",
		"Namespace":   t.Namespace,
	}, model.RouteRule.Type); err != nil {
		return err
	}
	if err := t.verifyFaultInjection("a", "c", "version", "v2", time.Second*5, 503); err != nil {
		return err
	}
	glog.Info("Success!")

	glog.Info("Testing redirect..")
	redirectHost := "b"
	redirectPath := "/new/path"
	if err := t.applyConfig("rule-redirect-injection.yaml.tmpl", map[string]string{
		"Source":       "a",
		"Destination":  "c",
		"HostRedirect": redirectHost,
		"Path":         redirectPath,
		"Namespace":    t.Namespace,
	}, model.RouteRule.Type); err != nil {
		return err
	}
	if err := t.verifyRedirect("a", "c", redirectHost, redirectPath, "testredirect", "enabled", 200); err != nil {
		return err
	}
	glog.Info("Success!")

	return nil
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
func (t *routing) verifyRouting(src, dst, headerKey, headerVal string,
	samples int, expectedCount map[string]int) error {
	url := fmt.Sprintf("http://%s/%s", dst, src)
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
