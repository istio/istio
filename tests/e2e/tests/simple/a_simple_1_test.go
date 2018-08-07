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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"istio.io/fortio/fhttp"
	"istio.io/fortio/periodic"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	baseDir        = "tests/e2e/tests/simple/testdata/"
	v1alpha3Subdir = "v1alpha3"

	servicesYaml         = "servicesToBeInjected.yaml"
	nonInjectedYaml      = "servicesNotInjected.yaml"
	routingR1Yaml        = "routingrule1.yaml"
	routingR2Yaml        = "routingrule2.yaml"
	routingRNPYaml       = "routingruleNoPods.yaml"
	timeToWaitForPods    = 20 * time.Second
	timeToWaitForIngress = 100 * time.Second
)

type testConfig struct {
	*framework.CommonConfig
}

var (
	tc        *testConfig
	testFlags = &framework.TestFlags{
		Ingress: true,
		Egress:  true,
	}
	versionSubdir = v1alpha3Subdir
)

func init() {
	testFlags.Init()
	flag.Parse()
}

func TestMain(m *testing.M) {
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
	url := getIngressOrGatewayOrFail(t) + "/fortio/debug"
	// Make sure the pods are running:
	if !util.CheckPodsRunning(tc.Kube.Namespace, tc.Kube.KubeConfig) {
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
			t.Errorf("Ingress not really ready after %v (%d tries)- last error is %d : %s", timeToWaitForIngress, i, code, dataDebug)
			return
		}
		code, data = fhttp.Fetch(o)
		dataDebug = fhttp.DebugSummary(data, 512) // not for searching, only for logging
		if code != 200 {
			log.Infof("Iter %d : ingress+svc not ready, code %d data %s", i, code, dataDebug)
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
				log.Infof("Pod %d %s not ready: %s", i, pod, res)
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
	res, err := util.Shell("kubectl exec -n %s %s -- /usr/local/bin/fortio curl http://echosrv.%s:8080/debug", ns, pod, ns)
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

func TestAuthWithHeaders(t *testing.T) {
	ns := tc.Kube.Namespace
	// Get the non istio pod
	podList, err := getPodList(ns, "app=fortio-noistio")
	if err != nil {
		t.Fatalf("kubectl failure to get non istio pod %v", err)
	}
	if len(podList) != 1 {
		t.Fatalf("Unexpected to get %d pods when expecting 1. got %v", len(podList), podList)
	}
	podNoIstio := podList[0]
	// Get the istio pod without extra unique port. We can't use the service name or cluster ip as
	// that vip only exists for declared host:port and the exploit relies on not having a listener
	// for that port.
	podList, err = getPodIPList(ns, "app=echosrv,extrap=non")
	if err != nil {
		t.Fatalf("kubectl failure to get deployment1 pod %v", err)
	}
	if len(podList) != 1 {
		t.Fatalf("Unexpected to get %d pod ips when expecting 1. got %v", len(podList), podList)
	}
	podIstioIP := podList[0]
	log.Infof("From client, non istio injected pod \"%s\" to istio pod \"%s\"", podNoIstio, podIstioIP)
	// TODO: ipv6 fix
	res, err := util.Shell("kubectl exec -n %s %s -- /usr/local/bin/fortio curl -H Host:echosrv-extrap.%s:8088 http://%s:8088/debug",
		ns, podNoIstio, ns, podIstioIP)
	if tc.Kube.AuthEnabled {
		if err == nil {
			t.Errorf("Running with auth on yet able to connect from non istio to istio (insecure): %v", res)
		} else {
			log.Infof("Got expected error with auth on and non istio->istio pod ip connection despite headers: %v", err)
		}
	} else {
		// even with auth off this should fail and not proxy from 1 pod to another based on host header
		if err == nil {
			t.Errorf("Running with auth off but yet able to connect from non istio to istio pod ip with wrong port: %v", res)
		} else {
			log.Infof("Got expected error from non istio to istio pod ip without auth but with wrong port: %v", err)
		}
	}
}

func Test503sDuringChanges(t *testing.T) {
	t.Skip("Skipping Test503sDuringChanges until bug #1038 is fixed") // TODO fix me!
	url := tc.Kube.IngressOrFail(t) + "/fortio/debug"
	rulePath1 := util.GetResourcePath(yamlPath(routingR1Yaml))
	rulePath2 := util.GetResourcePath(yamlPath(routingR2Yaml))
	go func() {
		time.Sleep(9 * time.Second)
		log.Infof("Changing rules mid run to v1/v2")
		if err := tc.Kube.Istioctl.CreateRule(rulePath1); err != nil {
			t.Errorf("istioctl rule create %s failed", yamlPath(routingR1Yaml))
			return
		}
		time.Sleep(4 * time.Second)
		log.Infof("Changing rules mid run to a/b")
		if err := tc.Kube.Istioctl.CreateRule(rulePath2); err != nil {
			t.Errorf("istioctl rule create %s failed", yamlPath(routingR1Yaml))
			return
		}
		time.Sleep(4 * time.Second)
		util.KubeDelete(tc.Kube.Namespace, rulePath1, tc.Kube.KubeConfig) // nolint:errcheck
		util.KubeDelete(tc.Kube.Namespace, rulePath2, tc.Kube.KubeConfig) // nolint:errcheck
	}()
	// run at a low/moderate QPS for a while while changing the routing rules,
	// check for any non 200s
	opts := fhttp.HTTPRunnerOptions{
		RunnerOptions: periodic.RunnerOptions{
			QPS:        24, // 3 per second per connection
			Duration:   30 * time.Second,
			NumThreads: 8,
		},
	}
	opts.URL = url
	res, err := fhttp.RunHTTPTest(&opts)
	if err != nil {
		t.Fatalf("Generating traffic via fortio failed: %v", err)
	}
	numRequests := res.DurationHistogram.Count
	num200s := res.RetCodes[http.StatusOK]
	if num200s != numRequests {
		t.Errorf("Not all %d requests were successful (%v)", numRequests, res.RetCodes)
	}
}

// This one may need to be fixed through some retries or health check
// config/setup/policy in envoy (through pilot)
func Test503sWithBadClusters(t *testing.T) {
	t.Skip("Skipping Test503sWithBadClusters until bug #1038 is fixed") // TODO fix me!
	url := tc.Kube.IngressOrFail(t) + "/fortio/debug"
	rulePath := util.GetResourcePath(yamlPath(routingRNPYaml))
	go func() {
		time.Sleep(9 * time.Second)
		log.Infof("Changing rules with some non existent destination, mid run")
		if err := tc.Kube.Istioctl.CreateRule(rulePath); err != nil {
			t.Errorf("istioctl create rule %s failed", yamlPath(routingRNPYaml))
			return
		}
	}()
	defer tc.Kube.Istioctl.DeleteRule(rulePath) // nolint:errcheck
	// run at a low/moderate QPS for a while while changing the routing rules,
	// check for limited number of errors
	opts := fhttp.HTTPRunnerOptions{
		RunnerOptions: periodic.RunnerOptions{
			QPS:        8,
			Duration:   20 * time.Second,
			NumThreads: 8,
		},
	}
	opts.URL = url
	res, err := fhttp.RunHTTPTest(&opts)
	if err != nil {
		t.Fatalf("Generating traffic via fortio failed: %v", err)
	}
	numRequests := res.DurationHistogram.Count
	num200s := res.RetCodes[http.StatusOK]
	numErrors := numRequests - num200s
	// 1 or a handful of 503s (1 per connection) is maybe ok, but not much more
	threshold := int64(opts.NumThreads) * 2
	if numErrors > threshold {
		t.Errorf("%d greater than threshold %d requests were successful (%v)", numRequests, threshold, res.RetCodes)
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

func getPodIPList(namespace string, selector string) ([]string, error) {
	pods, err := util.Shell("kubectl get pods -n %s -l %s -o jsonpath={.items[*].status.podIP}", namespace, selector)
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
			AppYamlTemplate: util.GetResourcePath(yamlPath(servicesYaml)),
			Template: &fortioTemplate{
				FortioImage: image,
			},
		},
		{
			KubeInject:      false,
			AppYamlTemplate: util.GetResourcePath(yamlPath(nonInjectedYaml)),
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

func getIngressOrGatewayOrFail(t *testing.T) string {
	return tc.Kube.IngressGatewayOrFail(t)
}

// yamlPath returns the appropriate yaml path
func yamlPath(filename string) string {
	return filepath.Join(baseDir, versionSubdir, filename)
}
