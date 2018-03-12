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
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang/sync/errgroup"
	"golang.org/x/net/context/ctxhttp"

	"istio.io/fortio/fhttp"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	servicesYaml    = "tests/e2e/tests/simple/servicesToBeInjected.yaml"
	nonInjectedYaml = "tests/e2e/tests/simple/servicesNotInjected.yaml"
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

// TODO: need an "is cluster ready" before running any tests

func TestSimpleIngress(t *testing.T) {
	// Tests the rewrite/dropping of the /fortio/ prefix as fortio only replies
	// with "echo debug server ..." on the /debug uri.
	url := tc.Kube.IngressOrFail(t) + "/fortio/debug"
	log.Infof("Fetching '%s'", url)

	retry := util.Retrier{
		BaseDelay:   time.Second,
		MaxDelay:    time.Second,
		Retries:     1000,
		MaxDuration: 105 * time.Second,
	}

	client := &http.Client{Timeout: 5 * time.Second}
	_, err := retry.Retry(context.Background(), func(ctx context.Context, i int) error {
		resp, err := ctxhttp.Get(ctx, client, url)
		if err != nil {
			log.Warnf("Attempt %d : http.Get error %v", i, err)
			return fmt.Errorf("attempt %d : http.Get error %v", i, err)
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Warnf("Attempt %d : ReadAll error %v", i, err)
			return fmt.Errorf("attempt %d : ReadAll error %v", i, err)
		}

		_ = resp.Body.Close()
		bodyStr := string(body)
		if len(bodyStr) == 0 {
			log.Infof("Attempt %d: reply body is empty", i)
			return fmt.Errorf("attempt %d: reply body is empty", i)
		}

		log.Infof("Attempt %d: reply is\n%s\n---END--", i, bodyStr)
		needle := "echo debug server up"
		if !strings.Contains(bodyStr, needle) {
			log.Warnf("Not finding expected %q in %q", needle, fhttp.DebugSummary(body, 128))
			return fmt.Errorf("not finding expected %q in %q", needle, fhttp.DebugSummary(body, 128))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err.Error())
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)

	log.Infof("Configuration readiness pre-check from %v to http://echosrv.%s:8080/echo", podList, ns)
	for i := range podList {
		pod := podList[i]
		g.Go(func() error {
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					_, err := util.ShellContext(ctx, "kubectl exec -n %s %s -c echosrv -- /usr/local/bin/fortio curl http://echosrv.%s:8080/echo", ns, pod, ns) // nolint: lll
					if err == nil {
						return nil
					}
					time.Sleep(time.Second)
				}
			}
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("Configuration readiness pre-check failed after %v", err)
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
			t.Fatalf("Running with auth on yet able to connect from non istio to istio (insecure): %v", res)
		} else {
			log.Infof("Got expected error with auth on and non istio->istio connection: %v", err)
		}
	} else {
		if err == nil {
			log.Infof("Got expected success with auth off and non istio->istio connection: %v", res)
		} else {
			t.Fatalf("Unexpected error connect from non istio to istio without auth: %v", err)
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
