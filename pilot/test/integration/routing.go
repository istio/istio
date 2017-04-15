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
	"istio.io/manager/model"
	"istio.io/manager/test/util"
)

type routing struct {
	*infra
}

const (
	defaultRoute = "default-route"
	contentRoute = "content-route"
	faultRoute   = "fault-route"
)

func (t *routing) String() string {
	return "routing rules"
}

func (t *routing) setup() error {
	return nil
}

func (t *routing) run() error {
	// First test default routing
	// Create a bytes buffer to hold the YAML form of rules
	glog.Info("Routing all traffic to world-v1 and verifying..")
	if err := t.applyConfig("rule-default-route.yaml.tmpl", map[string]string{
		"destination": "c",
		"Namespace":   t.Namespace,
	}, model.RouteRule, defaultRoute, "a"); err != nil {
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

	glog.Info("Routing 75 percent to world-v1, 25 percent to world-v2 and verifying..")
	if err := t.applyConfig("rule-weighted-route.yaml.tmpl", map[string]string{
		"destination": "c",
		"Namespace":   t.Namespace,
	}, model.RouteRule, defaultRoute, "a"); err != nil {
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

	glog.Info("Routing 100 percent to world-v2 using header based routing and verifying..")
	if err := t.applyConfig("rule-content-route.yaml.tmpl", map[string]string{
		"source":      "a",
		"destination": "c",
		"Namespace":   t.Namespace,
	}, model.RouteRule, contentRoute, "a"); err != nil {
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

	glog.Info("Testing fault injection..")
	if err := t.applyConfig("rule-fault-injection.yaml.tmpl", map[string]string{
		"source":      "a",
		"destination": "c",
		"Namespace":   t.Namespace,
	}, model.RouteRule, faultRoute, "a"); err != nil {
		return err
	}
	if err := t.verifyFaultInjection("a", "c", "version", "v2", time.Second*5, 503); err != nil {
		return err
	}
	glog.Info("Success!")

	return nil
}

func (t *routing) teardown() {
	glog.Info("Cleaning up route rules...")
	if err := util.Run("kubectl delete istioconfigs --all -n " + t.Namespace); err != nil {
		glog.Warning(err)
	}
}

// verifyRouting verifies if the traffic is split as specified across different deployments in a service
func (t *routing) verifyRouting(src, dst, headerKey, headerVal string,
	samples int, expectedCount map[string]int) error {
	count := make(map[string]int)
	for version := range expectedCount {
		count[version] = 0
	}

	url := fmt.Sprintf("http://%s/%s", dst, src)
	glog.Infof("Making %d requests (%s) from %s...\n", samples, url, src)

	cmd := fmt.Sprintf("kubectl exec %s -n %s -c app -- client -url %s -count %d -key %s -val %s",
		t.apps[src][0], t.Namespace, url, samples, headerKey, headerVal)
	request, err := util.Shell(cmd)
	glog.V(2).Info(request)
	if err != nil {
		return err
	}

	matches := regexp.MustCompile("ServiceVersion=(.*)").FindAllStringSubmatch(request, -1)
	for _, match := range matches {
		if len(match) > 1 {
			id := match[1]
			count[id]++
		}
	}

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
	cmd := fmt.Sprintf("kubectl exec %s -n %s -c app -- client -url %s -key %s -val %s",
		t.apps[src][0], t.Namespace, url, headerKey, headerVal)

	start := time.Now()
	request, err := util.Shell(cmd)
	glog.V(2).Info(request)
	elapsed := time.Since(start)
	if err != nil {
		return err
	}

	match := regexp.MustCompile("StatusCode=(.*)").FindStringSubmatch(request)
	statusCode := 0
	if len(match) > 1 {
		statusCode, err = strconv.Atoi(match[1])
		if err != nil {
			statusCode = -1
		}
	}

	// +/- 1s variance
	epsilon := time.Second * 2
	if elapsed > respTime+epsilon || elapsed < respTime-epsilon || respCode != statusCode {
		return fmt.Errorf("fault injection verification failed: "+
			"response time is %s with status code %d, "+
			"expected response time is %s +/- %s with status code %d", elapsed, statusCode, respTime, epsilon, respCode)
	}
	return nil
}

func (t *routing) addConfig(config []byte, kind, name string, create bool) error {
	glog.Infof("Add config %s", string(config))
	istioKind, ok := model.IstioConfig[kind]
	if !ok {
		return fmt.Errorf("Invalid kind %s", kind)
	}
	v, err := istioKind.FromYAML(string(config))
	check(err)
	key := model.Key{
		Kind:      kind,
		Name:      name,
		Namespace: t.Namespace,
	}
	if create {
		return istioClient.Post(key, v)
	}

	return istioClient.Put(key, v)
}

func (t *routing) applyConfig(inFile string, data map[string]string, kind, name, envoy string) error {
	config, err := fill(inFile, data)
	if err != nil {
		return err
	}
	_, exists := istioClient.Get(model.Key{Kind: kind, Name: name, Namespace: t.Namespace})
	if err := t.addConfig([]byte(config), kind, name, !exists); err != nil {
		return err
	}
	glog.Info("Sleeping for the config to propagate")
	time.Sleep(3 * time.Second)
	return nil
}
