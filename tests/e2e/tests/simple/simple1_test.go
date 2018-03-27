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

/*
Simple test - first time:
source istio.VERSION
bazel run //tests/e2e/tests/simple:go_default_test -- -test.v \
    -test.run TestSimple1 --skip_cleanup --auth_enable --namespace=e2e
After which to Retest:
bazel run //tests/e2e/tests/simple:go_default_test -- -test.v \
    -test.run TestSimple1 --skip_setup --skip_cleanup --auth_enable --namespace=e2e
*/

package simple

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"
	"time"

	"istio.io/fortio/fhttp"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	servicesYaml         = "tests/e2e/tests/simple/servicesToBeInjected.yaml"
	nonInjectedYaml      = "tests/e2e/tests/simple/servicesNotInjected.yaml"
	timeToWaitForPods    = 20 * time.Second
	timeToWaitForIngress = 100 * time.Second
)

type testConfig struct {
	*framework.CommonConfig
}

var (
	tc *testConfig
)

func TestMain(m *testing.M) {
	flag.Parse()
	if err := framework.InitLogging(); err != nil {
		panic("cannot setup logging")
	}
	if err := setTestConfig(); err != nil {
		log.Error("could not create TestConfig")
		os.Exit(-1)
	}
	os.Exit(tc.RunTest(m))
}

func TestSimpleIngress(t *testing.T) {
	// Tests that a simple ingress with rewrite/dropping of the /fortio/ prefix
	// works, as fortio only replies with "echo debug server ..." on the /debug uri.
	url := tc.Kube.IngressOrFail(t) + "/fortio/debug"
	// Make sure the pods are running:
	if !util.CheckPodsRunning(tc.Kube.Namespace) {
		t.Fatalf("Pods not ready!")
	}

	// This checks until all of those 3 things become ready:
	// 1) ingress pod is up (that done by IngressOrFail above)
	// 2) the destination pod (and its envoy) is up/reachable
	// 3) the ingress and routerules are propagated and active on the ingress
	log.Infof("Fetching '%s'", url)
	needle := []byte("echo debug server up")

	o := fhttp.NewHTTPOptions(url)
	var (
		code      int
		data      []byte
		dataDebug string
	)
	timeOut := time.Now().Add(timeToWaitForIngress)
	for i := 0; true; i++ {
		if time.Now().After(timeOut) {
			t.Errorf("Ingress not really ready after %v - last error is %d : %s", timeToWaitForIngress, code, dataDebug)
		}
		code, data = fhttp.Fetch(o)
		dataDebug = fhttp.DebugSummary(data, 512) // not for searching, only for logging
		if code != 200 {
			log.Infof("Iter %d : ingress+svc not ready, code %d data %s", code, dataDebug)
			time.Sleep(1 * time.Second)
			continue
		}
		if !bytes.Contains(data, needle) {
			t.Fatalf("Iter %d : unexpected content despite 200: %s", i, dataDebug)
		}
		// Test success:
		log.Infof("Iter %d : ingress->Svc is up! Found %s", i, dataDebug)
		return
	}
}

func TestSvc2Svc(t *testing.T) {
	ns := tc.Kube.Namespace
	// Get the 2 pods
	podList, err := getPodList(ns, "app=echosrv")
	if err != nil {
		t.Fatalf("kubectl failure to get pods %v", err)
	}
	if len(podList) != 2 {
		t.Fatalf("Unexpected to get %d pods when expecting 2. got %v", len(podList), podList)
	}
	// Check that both pods are ready (can talk to each other through envoy) before doing a (small) load test
	// (despite the CheckPodsRunning in previous test, envoy rules may not have reached the 2 pods envoy yet)
	log.Infof("Configuration readiness pre-check from %v to http://echosrv:8080/echo", podList)
	timeout := time.Now().Add(timeToWaitForPods)
	var res string
	for {
		if time.Now().After(timeout) {
			t.Fatalf("Pod readyness failed after %v - last error: %s", timeToWaitForPods, res)
		}
		ready := 0
		for i := range podList {
			pod := podList[i]
			res, err := util.Shell("kubectl exec -n %s %s -c echosrv -- /usr/local/bin/fortio curl http://echosrv:8080/echo", ns, pod)
			if err != nil {
				log.Infof("Pod %i %s not ready: %s", i, pod, res)
			} else {
				ready++
			}
		}
		if ready == len(podList) {
			log.Infof("All %d pods ready", ready)
			break
		}
		time.Sleep(time.Second)
	}
	// call into the service from each of the pods
	// TODO: use the fortio 0.3.1 web/api endpoint instead and get JSON results (across this file)
	for _, pod := range podList {
		log.Infof("From pod \"%s\"", pod)
		_, err := util.Shell("kubectl exec -n %s %s -c echosrv -- /usr/local/bin/fortio load -qps 0 -t 10s http://echosrv.%s:8080/echo", ns, pod, ns)
		if err != nil {
			t.Fatalf("kubectl failure to run fortio %v", err)
		}
	}
	// Success
}

// Readiness is shared with previous test, expected to run serially.
func TestAuth(t *testing.T) {
	ns := tc.Kube.Namespace
	// Get the 2 pods
	podList, err := getPodList(ns, "app=fortio-noistio")
	if err != nil {
		t.Fatalf("kubectl failure to get pods %v", err)
	}
	if len(podList) != 1 {
		t.Fatalf("Unexpected to get %d pods when expecting 1. got %v", len(podList), podList)
	}
	pod := podList[0]
	log.Infof("From client, non istio injected pod \"%s\"", pod)
	res, err := util.Shell("kubectl exec -n %s %s -- /usr/local/bin/fortio load -qps 5 -t 1s http://echosrv.%s:8080/echo", ns, pod, ns)
	if tc.Kube.AuthEnabled {
		if err == nil {
			t.Errorf("Running with auth on yet able to connect from non istio to istio (insecure): %v", res)
		} else {
			log.Infof("Got expected error with auth on and non istio->istio connection: %v", err)
		}
	} else {
		if err == nil {
			log.Infof("Got expected success with auth off and non istio->istio connection: %v", res)
		} else {
			t.Errorf("Unexpected error connect from non istio to istio without auth: %v", err)
		}
	}
}

type fortioTemplate struct {
	FortioImage string
}

func getPodList(namespace string, selector string) ([]string, error) {
	pods, err := util.Shell("kubectl get pods -n %s -l %s -o jsonpath={.items[*].metadata.name}", namespace, selector)
	if err != nil {
		return nil, err
	}
	return strings.Split(pods, " "), nil
}

func setTestConfig() error {
	cc, err := framework.NewCommonConfig("simple_auth_test")
	if err != nil {
		return err
	}
	tc = new(testConfig)
	tc.CommonConfig = cc
	hub := os.Getenv("FORTIO_HUB")
	tag := os.Getenv("FORTIO_TAG")
	image := hub + "/fortio:" + tag
	if hub == "" || tag == "" {
		image = "istio/fortio:latest_release"
	}
	log.Infof("Fortio hub %s tag %s -> image %s", hub, tag, image)
	services := []framework.App{
		{
			KubeInject:      true,
			AppYamlTemplate: util.GetResourcePath(servicesYaml),
			Template: &fortioTemplate{
				FortioImage: image,
			},
		},
		{
			KubeInject:      false,
			AppYamlTemplate: util.GetResourcePath(nonInjectedYaml),
			Template: &fortioTemplate{
				FortioImage: image,
			},
		},
	}
	for i := range services {
		tc.Kube.AppManager.AddApp(&services[i])
	}
	return nil
}
