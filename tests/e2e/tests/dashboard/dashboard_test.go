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
	"errors"
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
	mixerDashboard = "addons/grafana/dashboards/mixer-dashboard.json"
	pilotDashboard = "addons/grafana/dashboards/pilot-dashboard.json"
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
		"$adapter", "kubernetesenv",
		`connection_mtls=\"true\"`, "",
		`connection_mtls=\"false\"`, "",
		`\`, "",
	)

	tcpReplacer = strings.NewReplacer(
		"$tcp_destination", "netcat-srv.*",
		"istio-ingress", "netcat-client",
		"unknown", "v1.*",
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

func TestDashboards(t *testing.T) {

	t.Log("Validating prometheus in ready-state...")
	if err := waitForMetricsInPrometheus(t); err != nil {
		logMixerMetrics(t)
		t.Fatalf("Sentinel metrics never appeared in Prometheus: %v", err)
	}
	t.Log("Sentinel metrics found in prometheus.")

	cases := []struct {
		name      string
		dashboard string
		filter    func([]string) []string
	}{
		{"Istio", istioDashboard, func(queries []string) []string { return queries }},
		{"Mixer", mixerDashboard, mixerQueryFilterFn},
		{"Pilot", pilotDashboard, pilotQueryFilterFn},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			queries, err := extractQueries(testCase.dashboard)
			if err != nil {
				t.Fatalf("Could not extract queries from dashboard: %v", err)
			}

			queries = testCase.filter(queries)

			t.Logf("For %s, checking %d queries.", testCase.name, len(queries))

			for _, query := range queries {
				modified := replaceGrafanaTemplates(query)
				value, err := tc.promAPI.Query(context.Background(), modified, time.Now())
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

			if t.Failed() {
				logMixerMetrics(t)
			}
		})
	}
}

func sendTrafficToCluster(gateway string) (*fhttp.HTTPRunnerResults, error) {
	opts := fhttp.HTTPRunnerOptions{
		RunnerOptions: periodic.RunnerOptions{
			QPS:        10,
			Exactly:    60,
			NumThreads: 5,
			Out:        os.Stderr,
		},
		HTTPOptions: fhttp.HTTPOptions{
			URL: gateway + "/fortio/?status=404:10,503:15&size=1024:10,512:5",
		},
		AllowInitialErrors: true,
	}
	return fhttp.RunHTTPTest(&opts)
}

func generateTCPInCluster() error {
	ns := tc.Kube.Namespace
	ncPods, err := getPodList(ns, "app=netcat-client")
	if err != nil {
		return fmt.Errorf("could not get nc client pods: %v", err)
	}
	if len(ncPods) != 1 {
		return fmt.Errorf("bad number of pods returned for netcat client: %d", len(ncPods))
	}
	clientPod := ncPods[0]
	_, err = util.Shell("kubectl -n %s exec %s -c netcat -- sh -c \"echo 'test' | nc -vv -w5 -vv netcat-srv 44444\"", ns, clientPod)
	return err
}

func extractQueries(dashFile string) ([]string, error) {
	var queries []string
	dash, err := os.Open(util.GetResourcePath(dashFile))
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

// There currently is no good way to inject failures into a running Mixer,
// nor to cause Mixers to become unhealthy and drop out of cluster membership.
// For now, we will filter out the queries related to those metrics.
//
// Issue: https://github.com/istio/istio/issues/4070
func mixerQueryFilterFn(queries []string) []string {
	filtered := make([]string, 0, len(queries))
	for _, query := range queries {
		if strings.Contains(query, "_rq_5xx") {
			continue
		}
		if strings.Contains(query, "_rq_4xx") {
			continue
		}
		if strings.Contains(query, "ejections") {
			continue
		}
		if strings.Contains(query, "retry") {
			continue
		}
		if strings.Contains(query, "DataLoss") {
			continue
		}
		if strings.Contains(query, "grpc_code!=") {
			continue
		}
		filtered = append(filtered, query)
	}
	return filtered
}

// There currently is no good way to inject failures into a running Pilot,
// nor to cause Pilots to become unhealthy and drop out of cluster membership.
// For now, we will filter out the queries related to those metrics.
//
// Issue: https://github.com/istio/istio/issues/4155
func pilotQueryFilterFn(queries []string) []string {
	filtered := make([]string, 0, len(queries))
	for _, query := range queries {
		if strings.Contains(query, "cache_name") && strings.Contains(query, "sds") {
			continue
		}
		if strings.Contains(query, "_rq_5xx") {
			continue
		}
		if strings.Contains(query, "_rq_4xx") {
			continue
		}
		if strings.Contains(query, "_membership_") {
			continue
		}
		if strings.Contains(query, "discovery_errors") {
			continue
		}
		if strings.Contains(query, "update_failure") {
			continue
		}
		filtered = append(filtered, query)
	}
	return filtered
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
	promAPI v1.API
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
	if !util.CheckPodsRunning(tc.Kube.Namespace) {
		return fmt.Errorf("could not get all pods running")
	}

	pAPI, err := promAPI()
	if err != nil {
		return fmt.Errorf("could not build prometheus API client: %v", err)
	}
	t.promAPI = pAPI

	if err = waitForMixerConfigResolution(); err != nil {
		return fmt.Errorf("mixer never received configuration: %v", err)
	}

	gateway, errGw := tc.Kube.Ingress()
	if errGw != nil {
		return errGw
	}
	if _, err := sendTrafficToCluster(gateway); err != nil {
		return fmt.Errorf("generating HTTP traffic failed: %v", err)
	}

	if err := generateTCPInCluster(); err != nil {
		return fmt.Errorf("generating TCP traffic failed: %v", err)
	}

	allowPrometheusSync()

	return nil
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
	if !util.CheckPodsRunning(tc.Kube.Namespace) {
		return errors.New("could not establish port-forward to prometheus: pods not running")
	}

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
	time.Sleep(1 * time.Minute)
}

var waitDurations = []time.Duration{0, 5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute}

func waitForMixerConfigResolution() error {
	configQuery := `sum(mixer_config_handler_config_count)`
	handlers := 0.0
	for _, duration := range waitDurations {
		log.Infof("waiting for Mixer to be configured with correct handlers: %v", duration)
		time.Sleep(duration)
		val, err := getMetricValue(configQuery)
		if err == nil && val >= 3.0 {
			return nil
		}
		handlers = val
	}
	return fmt.Errorf("incorrect number of handlers known in Mixer; got %f, want >= 3.0", handlers)
}

func waitForMetricsInPrometheus(t *testing.T) error {
	// These are sentinel metrics that will be used to evaluate if prometheus
	// scraping has occurred and data is available via promQL.
	queries := []string{
		`round(sum(irate(istio_request_count[1m])), 0.001)`,
		`sum(irate(istio_request_count{response_code=~"4.*"}[1m]))`,
		`sum(irate(istio_request_count{response_code=~"5.*"}[1m]))`,
	}

	for _, duration := range waitDurations {
		t.Logf("Waiting for prometheus metrics: %v", duration)
		time.Sleep(duration)

		i := 0
		l := len(queries)
		for i < l {
			if metricHasValue(queries[i]) {
				t.Logf("Value found for: '%s'", queries[i])
				queries = append(queries[:i], queries[i+1:]...)
				l--
				continue
			}
			t.Logf("No value found for: '%s'", queries[i])
			i++
		}

		if len(queries) == 0 {
			return nil
		}
	}

	return fmt.Errorf("no values found for: %#v", queries)
}

func metricHasValue(query string) bool {
	_, err := getMetricValue(query)
	return err == nil
}

func getMetricValue(query string) (float64, error) {
	value, err := tc.promAPI.Query(context.Background(), query, time.Now())
	if err != nil || value == nil {
		return 0, fmt.Errorf("could not retrieve a value for metric '%s': %v", query, err)
	}
	switch v := value.(type) {
	case model.Vector:
		if v.Len() < 1 {
			return 0, fmt.Errorf("no values for metric: '%s'", query)
		}
		return float64(v[0].Value), nil
	case *model.Scalar:
		return float64(v.Value), nil
	}
	return 0, fmt.Errorf("no known value for metric: '%s'", query)
}

func logMixerMetrics(t *testing.T) {
	ns := tc.Kube.Namespace
	pods, err := getPodList(ns, "app=echosrv")
	if err != nil || len(pods) < 1 {
		t.Logf("Failure getting mixer metrics: %v", err)
		return
	}
	resp, err := util.Shell("kubectl exec -n %s %s -c echosrv -- /usr/local/bin/fortio curl http://istio-mixer.%s:42422/metrics", ns, pods[0], ns)
	if err != nil {
		t.Logf("could not retrieve metrics: %v", err)
		return
	}
	t.Logf("GET /metrics:\n%v", resp)

}
