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
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"fortio.org/fortio/fhttp"
	"fortio.org/fortio/periodic"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	istioMeshDashboard = "install/kubernetes/helm/istio/charts/grafana/dashboards/istio-mesh-dashboard.json"
	serviceDashboard   = "install/kubernetes/helm/istio/charts/grafana/dashboards/istio-service-dashboard.json"
	workloadDashboard  = "install/kubernetes/helm/istio/charts/grafana/dashboards/istio-workload-dashboard.json"
	mixerDashboard     = "install/kubernetes/helm/istio/charts/grafana/dashboards/mixer-dashboard.json"
	pilotDashboard     = "install/kubernetes/helm/istio/charts/grafana/dashboards/pilot-dashboard.json"
	galleyDashboard    = "install/kubernetes/helm/istio/charts/grafana/dashboards/galley-dashboard.json"
	fortioYaml         = "tests/e2e/tests/dashboard/fortio-rules.yaml"
	netcatYaml         = "tests/e2e/tests/dashboard/netcat-rules.yaml"

	prometheusPort = uint16(9090)
)

var (
	replacer = strings.NewReplacer(
		"$workload", "echosrv.*",
		"$service", "echosrv.*",
		"$dstsvc", "echosrv.*",
		"$srcwl", "istio-ingressgateway.*",
		"$adapter", "kubernetesenv",
		`connection_security_policy=\"unknown\"`, "",
		`connection_security_policy=\"mutual_tls\"`, "",
		`connection_security_policy!=\"mutual_tls\"`, "",
		`source_workload_namespace=~\"$srcns\"`, "",
		`destination_workload_namespace=~\"$dstns\"`, "",
		`source_workload_namespace=~\"$namespace\"`, "",
		`destination_workload_namespace=~\"$namespace\"`, "",
		`destination_workload=~\"$dstwl\"`, "",
		`\`, "",
	)

	tcpReplacer = strings.NewReplacer(
		"echosrv", "netcat-srv",
		"istio-ingressgateway", "netcat-client",
	)

	workloadReplacer = strings.NewReplacer(
		`source_workload=~"netcat-srv.*"`, `source_workload=~"netcat-client.*"`,
		`source_workload=~"echosrv.*"`, `source_workload=~"istio-ingressgateway.*"`,
	)

	queryCleanupReplacer = strings.NewReplacer(
		"{, ,", "{",
		"{,", "{",
		", , ,", ",",
		", ,", ",",
		",,", ",",
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
		logMixerInfo(t, "istio-telemetry", 42422)
		t.Fatalf("Sentinel metrics never appeared in Prometheus: %v", err)
	}
	t.Log("Sentinel metrics found in prometheus.")

	cases := []struct {
		name           string
		dashboard      string
		filter         func([]string) []string
		customReplacer *strings.Replacer
		metricHost     string
		metricPort     int
	}{
		{"Istio", istioMeshDashboard, func(queries []string) []string { return queries }, nil, "istio-telemetry", 42422},
		{"Service", serviceDashboard, func(queries []string) []string { return queries }, nil, "istio-telemetry", 42422},
		{"Workload", workloadDashboard, func(queries []string) []string { return queries }, workloadReplacer, "istio-telemetry", 42422},
		{"Mixer", mixerDashboard, mixerQueryFilterFn, nil, "istio-telemetry", 9093},
		{"Pilot", pilotDashboard, pilotQueryFilterFn, nil, "istio-pilot", 9093},
		{"Galley", galleyDashboard, galleyQueryFilterFn, nil, "istio-galley", 9093},
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
				if testCase.customReplacer != nil {
					modified = testCase.customReplacer.Replace(modified)
				}
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
				logMixerMetrics(t, testCase.metricHost, testCase.metricPort)
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
			URL: gateway + "/?status=418:10,520:15&size=1024:10,512:5",
		},
		AllowInitialErrors: true,
	}
	return fhttp.RunHTTPTest(&opts)
}

func generateTCPInCluster() error {
	ns := tc.Kube.Namespace
	ncPods, err := podList(ns, "app=netcat-client")
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
	return queryCleanupReplacer.Replace(out)
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
		if strings.Contains(query, "pilot_xds_push_errors") {
			continue
		}
		if strings.Contains(query, "_reject") {
			continue
		}
		filtered = append(filtered, query)
	}
	return filtered
}

func galleyQueryFilterFn(queries []string) []string {
	filtered := make([]string, 0, len(queries))
	for _, query := range queries {
		if strings.Contains(query, "validation_cert_key_update_errors") {
			continue
		}
		if strings.Contains(query, "validation_failed") {
			continue
		}
		if strings.Contains(query, "validation_http_error") {
			continue
		}
		filtered = append(filtered, query)
	}
	return filtered
}

func promAPI() (v1.API, error) {
	client, err := api.NewClient(api.Config{Address: fmt.Sprintf("http://localhost:%d", prometheusPort)})
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
		image = "fortio/fortio:latest" // TODO: change
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
	if !util.CheckPodsRunning(tc.Kube.Namespace, tc.Kube.KubeConfig) {
		return fmt.Errorf("could not get all pods running")
	}

	pAPI, err := promAPI()
	if err != nil {
		return fmt.Errorf("could not build prometheus API client: %v", err)
	}
	t.promAPI = pAPI

	if err = waitForMixerConfigResolution(); err != nil {
		logMixerLogs(pkgLogger{})
		return fmt.Errorf("mixer never received configuration: %v", err)
	}

	if err = waitForMixerProxyReadiness(); err != nil {
		return fmt.Errorf("mixer's proxy never was ready to serve traffic: %v", err)
	}

	gateway, errGw := tc.Kube.IngressGateway()
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
	portFwdProcesses []kube.PortForwarder
}

func newPromProxy(namespace string) *promProxy {
	return &promProxy{
		namespace: namespace,
	}
}

func (p *promProxy) portForward(labelSelector string, localPort uint16, remotePort uint16) error {
	log.Infof("Setting up %s proxy", labelSelector)
	options := &kube.PodSelectOptions{
		PodNamespace:  p.namespace,
		LabelSelector: labelSelector,
	}
	accessor, err := kube.NewAccessor(tc.Kube.KubeConfig)
	if err != nil {
		log.Errorf("Error creating accessor: %v", err)
		return err
	}
	forwarder, err := accessor.NewPortForwarder(options, localPort, remotePort)
	if err != nil {
		log.Errorf("Error creating port forwarder: %v", err)
		return err
	}
	if err := forwarder.Start(); err != nil {
		log.Errorf("Error starting port forwarder: %v", err)
		return err
	}

	p.portFwdProcesses = append(p.portFwdProcesses, forwarder)
	log.Infof("running %s port-forward in background", labelSelector)
	return nil
}

func (p *promProxy) Setup() error {
	if !util.CheckPodsRunning(tc.Kube.Namespace, tc.Kube.KubeConfig) {
		return errors.New("could not establish port-forward to prometheus: pods not running")
	}

	return p.portForward("app=prometheus", prometheusPort, prometheusPort)
}

func (p *promProxy) Teardown() (err error) {
	log.Info("Cleaning up mixer proxy")
	for _, pf := range p.portFwdProcesses {
		pf.Close()
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

func podList(namespace string, selector string) ([]string, error) {
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
	// we are looking for confirmation that 3 handlers were configured and that none of them had
	// build failures

	configQuery := `max(mixer_config_handler_configs_total) - max(mixer_handler_handler_build_failures_total or up * 0)`
	handlers := 0.0
	for _, duration := range waitDurations {
		log.Infof("Waiting for Mixer to be configured with correct handlers: %v", duration)
		time.Sleep(duration)
		val, err := metricValue(configQuery)
		if err == nil && val >= 3.0 {
			return nil
		}
		handlers = val
	}
	return fmt.Errorf("incorrect number of handlers known in Mixer; got %f, want >= 3.0", handlers)
}

func waitForMixerProxyReadiness() error {
	mixerPods, err := podList(tc.Kube.Namespace, "istio-mixer-type=telemetry")
	if err != nil {
		return fmt.Errorf("could not find Mixer pod: %v", err)
	}

	for _, duration := range waitDurations {
		log.Infof("Waiting for Mixer's proxy to be ready to dispatch traffic: %v", duration)
		time.Sleep(duration)

		for _, pod := range mixerPods {
			options := &kube.PodSelectOptions{
				PodNamespace: tc.Kube.Namespace,
				PodName:      pod,
			}

			accessor, err := kube.NewAccessor(tc.Kube.KubeConfig)
			if err != nil {
				log.Errorf("Error creating accessor: %v", err)
				return err
			}
			forwarder, err := accessor.NewPortForwarder(options, 16000, 15000)
			if err != nil {
				log.Infof("Error creating port forwarder: %v", err)
				continue
			}
			if err := forwarder.Start(); err != nil {
				log.Infof("Error starting port forwarder: %v", err)
				continue
			}

			resp, err := http.Get("http://localhost:16000/server_info")
			forwarder.Close()

			if err != nil {
				log.Infof("Failure retrieving status for pod %s (container: istio-proxy): %v", pod, err)
				continue
			}

			if resp.StatusCode == 200 {
				return nil
			}

			log.Infof("Failure retrieving status for pod %s (container: istio-proxy) status code should be 200 got: %d", pod, resp.StatusCode)
		}
	}
	return errors.New("proxy for mixer never started main dispatch loop")
}

func waitForMetricsInPrometheus(t *testing.T) error {
	// These are sentinel metrics that will be used to evaluate if prometheus
	// scraping has occurred and data is available via promQL.
	queries := []string{
		`round(sum(irate(istio_requests_total[1m])), 0.001)`,
		`sum(irate(istio_requests_total{response_code=~"4.*"}[1m]))`,
		`sum(irate(istio_requests_total{response_code=~"5.*"}[1m]))`,
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
	_, err := metricValue(query)
	return err == nil
}

func metricValue(query string) (float64, error) {
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

func logMixerInfo(t *testing.T, service string, port int) {
	logMixerMetrics(t, service, port)
	logMixerLogs(t)
}

func logMixerMetrics(t *testing.T, service string, port int) {
	ns := tc.Kube.Namespace
	pods, err := podList(ns, "app=echosrv")
	if err != nil || len(pods) < 1 {
		t.Logf("Failure getting metrics for '%s:%d': %v", service, port, err)
		return
	}
	resp, err := util.ShellMuteOutput("kubectl exec -n %s %s -c echosrv -- fortio curl http://%s.%s:%d/metrics", ns, pods[0], service, ns, port)
	if err != nil {
		t.Logf("could not retrieve metrics: %v", err)
		return
	}
	t.Logf("GET http://%s.%s:%d/metrics:\n%v", service, ns, port, resp)
}

type logger interface {
	Logf(fmt string, args ...interface{})
}

type pkgLogger struct{}

func (p pkgLogger) Logf(fmt string, args ...interface{}) {
	log.Infof(fmt, args...)
}

func logMixerLogs(l logger) {
	mixerPods, err := podList(tc.Kube.Namespace, "istio-mixer-type=telemetry")
	if err != nil {
		l.Logf("Could not retrieve Mixer logs: %v", err)
		return
	}
	for _, pod := range mixerPods {
		logs, err := util.ShellMuteOutput(fmt.Sprintf("kubectl logs %s -n %s -c mixer", pod, tc.Kube.Namespace))
		if err != nil {
			l.Logf("Failure retrieving logs for pod %s: %v", pod, err)
			continue
		}
		l.Logf("Mixer Container Logs for pod %s: \n%s", pod, logs)
	}
}
