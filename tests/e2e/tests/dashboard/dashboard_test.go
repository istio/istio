// Copyright 2018 Istio Authors
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

// Package dashboard provides testing of the grafana dashboards used in Istio
// to provide mesh monitoring capabilities.
package dashboard

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"istio.io/fortio/fhttp"
	"istio.io/fortio/periodic"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	istioDashboard = "addons/grafana/dashboards/istio-dashboard.json"
	fortioYaml     = "tests/e2e/tests/dashboard/fortio-rules.yaml"
	netcatYaml     = "tests/e2e/tests/dashboard/netcat-rules.yaml"

	prometheusPort = "9090"
)

var (
	replacer = strings.NewReplacer(
		"$source_version", "unknown",
		"$source", "istio-ingress.*",
		"$http_destination", "echosrv.*",
		"$destination_version", "v1.*",
		`\`, "",
	)

	tcpReplacer = strings.NewReplacer(
		"$tcp_destination", "netcat-srv.*",
		"istio-ingress", "netcat-client",
		"unknown", "v1",
	)

	tc *testConfig
)

func TestMain(m *testing.M) {
	flag.Parse()
	check(framework.InitLogging(), "cannot setup logging")
	check(setTestConfig(), "could not create TestConfig")
	tc.Cleanup.RegisterCleanable(tc)
	os.Exit(tc.RunTest(m))
}

func TestIstioDashboard(t *testing.T) {
	promAPI, err := promAPI()
	if err != nil {
		t.Fatalf("Could not build prometheus API client: %v", err)
	}

	t.Log("Generating TCP traffic...")
	if err = sendTCPTrafficToCluster(t); err != nil {
		t.Fatalf("Generating traffic failed: %v", err)
	}

	t.Log("Generating HTTP traffic...")
	_, err = sendTrafficToCluster()
	if err != nil {
		t.Fatalf("Generating traffic failed: %v", err)
	}

	t.Log("Sleep to allow prometheus scraping to happen...")
	allowPrometheusSync()

	queries, err := extractQueries()
	if err != nil {
		t.Fatalf("Could not extract queries from dashboard: %v", err)
	}

	for _, query := range queries {
		modified := replaceGrafanaTemplates(query)
		value, err := promAPI.Query(context.Background(), modified, time.Now())
		if err != nil {
			t.Errorf("Failure executing query (%s): %v", modified, err)
		}
		if value == nil {
			t.Errorf("Returned value should not be nil for '%s'", modified)
			continue
		}
		numSamples := 0
		switch v := value.(type) {
		case model.Vector:
			numSamples = v.Len()
		case *model.Scalar:
			numSamples = 1
		default:
			t.Logf("unknown metric value type: %T", v)
		}
		if numSamples == 0 {
			t.Errorf("Expected a metric value for '%s', found no samples: %#v", modified, value)
		}
	}
}

func sendTrafficToCluster() (*fhttp.HTTPRunnerResults, error) {
	opts := fhttp.HTTPRunnerOptions{
		RunnerOptions: periodic.RunnerOptions{
			QPS:        10,
			Exactly:    60,
			NumThreads: 5,
			Out:        os.Stderr,
		},
		HTTPOptions: fhttp.HTTPOptions{
			URL: "http://" + tc.Kube.Ingress + "/fortio/?status=404:10,503:15&size=1024:10,512:5",
		},
		AllowInitialErrors: true,
	}
	return fhttp.RunHTTPTest(&opts)
}

func sendTCPTrafficToCluster(t *testing.T) error {
	ns := tc.Kube.Namespace
	ncPods, err := getPodList(ns, "app=netcat-client")
	if err != nil {
		return fmt.Errorf("could not get nc client pods: %v", err)
	}
	if len(ncPods) != 1 {
		return fmt.Errorf("bad number of pods returned for netcat client: %d", len(ncPods))
	}
	clientPod := ncPods[0]
	res, err := util.Shell("kubectl -n %s exec %s -c netcat -- sh -c \"echo 'test' | nc -vv -w5 -vv netcat-srv 44444\"", ns, clientPod)
	t.Logf("TCP Traffic Response: %v", res)
	return err
}

func extractQueries() ([]string, error) {
	var queries []string
	dash, err := os.Open(util.GetResourcePath(istioDashboard))
	if err != nil {
		return queries, fmt.Errorf("could not open dashboard file: %v", err)
	}
	scanner := bufio.NewScanner(dash)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "expr") {
			query := strings.Trim(strings.TrimPrefix(strings.TrimSpace(line), "\"expr\": "), `",`)
			queries = append(queries, query)
		}
	}
	return queries, nil
}

func replaceGrafanaTemplates(orig string) string {
	out := replacer.Replace(orig)
	if strings.Contains(orig, "istio_tcp") {
		out = tcpReplacer.Replace(out)
	}
	return out
}

func promAPI() (v1.API, error) {
	client, err := api.NewClient(api.Config{Address: fmt.Sprintf("http://localhost:%s", prometheusPort)})
	if err != nil {
		return nil, err
	}
	return v1.NewAPI(client), nil
}

type testConfig struct {
	*framework.CommonConfig
	gateway string
}

func (t *testConfig) Teardown() error {
	return nil
}

func setTestConfig() error {
	cc, err := framework.NewCommonConfig("dashboard_test")
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
	log.Infof("Fortio hub %s tag %s -> image %s", hub, tag, image)
	services := []framework.App{
		{
			KubeInject:      true,
			AppYamlTemplate: util.GetResourcePath(fortioYaml),
			Template: &fortioTemplate{
				FortioImage: image,
			},
		},
		{
			KubeInject:      true,
			AppYamlTemplate: util.GetResourcePath(netcatYaml),
		},
	}
	for i := range services {
		tc.Kube.AppManager.AddApp(&services[i])
	}
	mp := newPromProxy(tc.Kube.Namespace)
	tc.Cleanup.RegisterCleanable(mp)
	return nil
}

func (t *testConfig) Setup() (err error) {
	t.gateway = "http://" + tc.Kube.Ingress
	if !util.CheckPodsRunning(tc.Kube.Namespace) {
		return fmt.Errorf("can't get all pods running")
	}
	return
}

type promProxy struct {
	namespace        string
	portFwdProcesses []*os.Process
}

func newPromProxy(namespace string) *promProxy {
	return &promProxy{
		namespace: namespace,
	}
}

func (p *promProxy) portForward(labelSelector string, localPort string, remotePort string) error {
	var pod string
	var err error
	var proc *os.Process

	getName := fmt.Sprintf("kubectl -n %s get pod -l %s -o jsonpath='{.items[0].metadata.name}'", p.namespace, labelSelector)
	pod, err = util.Shell(getName)
	if err != nil {
		return err
	}
	log.Infof("%s pod name: %s", labelSelector, pod)

	log.Infof("Setting up %s proxy", labelSelector)
	portFwdCmd := fmt.Sprintf("kubectl port-forward %s %s:%s -n %s", strings.Trim(pod, "'"), localPort, remotePort, p.namespace)
	log.Info(portFwdCmd)
	if proc, err = util.RunBackground(portFwdCmd); err != nil {
		log.Errorf("Failed to port forward: %s", err)
		return err
	}
	p.portFwdProcesses = append(p.portFwdProcesses, proc)
	log.Infof("running %s port-forward in background, pid = %d", labelSelector, proc.Pid)
	return nil
}

func (p *promProxy) Setup() error {
	return p.portForward("app=prometheus", prometheusPort, prometheusPort)
}

func (p *promProxy) Teardown() (err error) {
	log.Info("Cleaning up mixer proxy")
	for _, proc := range p.portFwdProcesses {
		err := proc.Kill()
		if err != nil {
			log.Errorf("Failed to kill port-forward process, pid: %d", proc.Pid)
		}
	}
	return
}

func check(err error, msg string) {
	if err != nil {
		log.Errorf("%s. Error %s", msg, err)
		os.Exit(-1)
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

func allowPrometheusSync() {
	time.Sleep(30 * time.Second)
}
