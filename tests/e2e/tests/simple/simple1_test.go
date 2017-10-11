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
bazel run //tests/e2e/tests/simple:go_default_test -- -alsologtostderr -test.v -v 2 \
    -test.run TestSimple1 --skip_cleanup --auth_enable --namespace=e2e
After which to Retest:
bazel run //tests/e2e/tests/simple:go_default_test -- -alsologtostderr -test.v -v 2 \
    -test.run TestSimple1 --skip_setup --skip_cleanup --auth_enable --namespace=e2e
*/

package simple

import (
	"flag"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang/glog"

	"istio.io/fortio/fhttp"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/e2e/util"
)

const (
	servicesYaml    = "tests/e2e/tests/simple/servicesToBeInjected.yaml"
	nonInjectedYaml = "tests/e2e/tests/simple/servicesNotInjected.yaml"
	routingR1Yaml   = "tests/e2e/tests/simple/routingrule1.yaml"
	routingR2Yaml   = "tests/e2e/tests/simple/routingrule2.yaml"
	routingRNPYaml  = "tests/e2e/tests/simple/routingruleNoPods.yaml"
)

type testConfig struct {
	*framework.CommonConfig
}

var (
	tc *testConfig
)

func TestMain(m *testing.M) {
	flag.Parse()
	if err := framework.InitGlog(); err != nil {
		panic("cannot setup glog")
	}
	if err := setTestConfig(); err != nil {
		glog.Fatal("could not create TestConfig")
	}
	os.Exit(tc.RunTest(m))
}

func TestSimpleIngress(t *testing.T) {
	// Tests the rewrite/dropping of the /fortio/ prefix as fortio only replies
	// with "echo debug server ..." on the /debug uri.
	url := "http://" + tc.Kube.Ingress + "/fortio/debug"
	glog.Infof("Fetching '%s'", url)
	attempts := 7 // should not take more than 70s to be live...
	for i := 1; i <= attempts; i++ {
		if i > 1 {
			time.Sleep(10 * time.Second) // wait between retries
		}
		resp, err := http.Get(url)
		if err != nil {
			glog.Warningf("Attempt %d : http.Get error %v", i, err)
			continue
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			glog.Warningf("Attempt %d : ReadAll error %v", i, err)
			continue
		}
		_ = resp.Body.Close()
		bodyStr := string(body)
		glog.Infof("Attempt %d: reply is\n%s\n---END--", i, bodyStr)
		needle := "echo debug server on echosrv"
		if !strings.Contains(bodyStr, needle) {
			glog.Warningf("Not finding expected %s in %s", needle, fhttp.DebugSummary(body, 128))
			continue
		}
		return // success
	}
	t.Errorf("Unable to find expected output after %d attempts", attempts)
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
	// call into the service from each of the pods
	for _, pod := range podList {
		glog.Infof("From pod \"%s\"", pod)
		_, err := util.Shell("kubectl exec -n %s %s -c echosrv -- /usr/local/bin/fortio load -qps 0 -t 10s http://echosrv.%s:8080/echo", ns, pod, ns)
		if err != nil {
			t.Fatalf("kubectl failure to run fortio %v", err)
		}
	}
	// Success
}

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
	glog.Infof("From client, non istio injected pod \"%s\"", pod)
	res, err := util.Shell("kubectl exec -n %s %s -- /usr/local/bin/fortio load -qps 5 -t 1s http://echosrv.%s:8080/echo", ns, pod, ns)
	if tc.Kube.AuthEnabled {
		if err == nil {
			t.Fatalf("Running with auth on yet able to connect from non istio to istio (insecure): %v", res)
		} else {
			glog.Infof("Got expected error with auth on and non istio->istio connection: %v", err)
		}
	} else {
		if err == nil {
			glog.Infof("Got expected success with auth off and non istio->istio connection: %v", res)
		} else {
			t.Fatalf("Unexpected error connect from non istio to istio without auth: %v", err)
		}
	}
}

func Test404sDuringChanges(t *testing.T) {
	url := "http://" + tc.Kube.Ingress + "/fortio/debug"
	rulePath1 := util.GetResourcePath(routingR1Yaml)
	rulePath2 := util.GetResourcePath(routingR2Yaml)
	go func() {
		time.Sleep(9 * time.Second)
		glog.Infof("Changing rules mid run to v1/v2")
		if err := tc.Kube.Istioctl.CreateRule(rulePath1); err != nil {
			t.Errorf("istioctl rule create %s failed", routingR1Yaml)
			return
		}
		time.Sleep(4 * time.Second)
		glog.Infof("Changing rules mid run to a/b")
		if err := tc.Kube.Istioctl.CreateRule(rulePath2); err != nil {
			t.Errorf("istioctl rule create %s failed", routingR1Yaml)
			return
		}
		time.Sleep(4 * time.Second)
		util.KubeDelete(tc.Kube.Namespace, rulePath1) // nolint:errcheck
		util.KubeDelete(tc.Kube.Namespace, rulePath2) // nolint:errcheck
	}()
	// run at a low/moderate QPS for a while while changing the routing rules,
	// check for any non 200s
	opts := fortio.HTTPRunnerOptions{
		RunnerOptions: fortio.RunnerOptions{
			QPS:        8,
			Duration:   20 * time.Second,
			NumThreads: 8,
		},
		URL: url,
	}
	res, err := fortio.RunHTTPTest(&opts)
	if err != nil {
		t.Fatalf("Generating traffic via fortio failed: %v", err)
	}
	numRequests := res.DurationHistogram.Count
	num200s := res.RetCodes[http.StatusOK]
	if num200s != numRequests {
		t.Errorf("Not all %d requests were successful (%v)", numRequests, res.RetCodes)
	}
}

func Test503sWithPartialRules(t *testing.T) {
	url := "http://" + tc.Kube.Ingress + "/fortio/debug"
	rulePath := util.GetResourcePath(routingRNPYaml)
	go func() {
		time.Sleep(9 * time.Second)
		glog.Infof("Changing rules with some non existent destination, mid run")
		if err := tc.Kube.Istioctl.CreateRule(rulePath); err != nil {
			t.Errorf("istiocrl create rule %s failed", routingRNPYaml)
			return
		}
	}()
	defer tc.Kube.Istioctl.DeleteRule(rulePath) // nolint:errcheck
	// run at a low/moderate QPS for a while while changing the routing rules,
	// check for any non 200s
	opts := fortio.HTTPRunnerOptions{
		RunnerOptions: fortio.RunnerOptions{
			QPS:        8,
			Duration:   20 * time.Second,
			NumThreads: 8,
		},
		URL: url,
	}
	res, err := fortio.RunHTTPTest(&opts)
	if err != nil {
		t.Fatalf("Generating traffic via fortio failed: %v", err)
	}
	numRequests := res.DurationHistogram.Count
	num200s := res.RetCodes[http.StatusOK]
	// 1 or a handful of 503s is maybe ok, but not 1/2
	if num200s != numRequests {
		t.Errorf("Not all %d requests were successful (%v)", numRequests, res.RetCodes)
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
		image = "istio/fortio:latest" // TODO: change
	}
	glog.Infof("Fortio hub %s tag %s -> image %s", hub, tag, image)
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
