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

// Package mixer defines integration tests that validate working mixer
// functionality in context of a test Istio-enabled cluster.
package mixer

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"istio.io/istio/devel/fortio"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/e2e/util"
)

const (
	bookinfoYaml             = "samples/bookinfo/kube/bookinfo.yaml"
	bookinfoRatingsv2Yaml    = "samples/bookinfo/kube/bookinfo-ratings-v2.yaml"
	bookinfoDbYaml           = "samples/bookinfo/kube/bookinfo-db.yaml"
	rulesDir                 = "samples/bookinfo/kube"
	rateLimitRule            = "mixer-rule-ratings-ratelimit.yaml"
	denialRule               = "mixer-rule-ratings-denial.yaml"
	newTelemetryRule         = "mixer-rule-additional-telemetry.yaml"
	routeAllRule             = "route-rule-all-v1.yaml"
	routeReviewsVersionsRule = "route-rule-reviews-v2-v3.yaml"
	routeReviewsV3Rule       = "route-rule-reviews-v3.yaml"
	tcpDbRule                = "route-rule-ratings-db.yaml"

	prometheusPort   = "9090"
	mixerMetricsPort = "42422"
	productPagePort  = "10000"

	destLabel         = "destination_service"
	responseCodeLabel = "response_code"

	// This namespace is used by default in all mixer config documents.
	// It will be replaced with the test namespace.
	templateNamespace = "istio-config-default"
)

type testConfig struct {
	*framework.CommonConfig
	gateway  string
	rulesDir string
}

var (
	tc                 *testConfig
	productPageTimeout = 60 * time.Second
	rules              = []string{rateLimitRule, denialRule, newTelemetryRule, routeAllRule,
		routeReviewsVersionsRule, routeReviewsV3Rule, tcpDbRule}
)

func (t *testConfig) Setup() (err error) {
	defer func() {
		if err != nil {
			dumpK8Env()
		}
	}()

	t.gateway = "http://" + tc.Kube.Ingress
	var srcBytes []byte
	for _, rule := range rules {
		src := util.GetResourcePath(filepath.Join(rulesDir, rule))
		dest := filepath.Join(t.rulesDir, rule)
		srcBytes, err = ioutil.ReadFile(src)
		if err != nil {
			glog.Errorf("Failed to read original rule file %s", src)
			return err
		}
		err = ioutil.WriteFile(dest, srcBytes, 0600)
		if err != nil {
			glog.Errorf("Failed to write into new rule file %s", dest)
			return err
		}
	}

	err = createDefaultRoutingRules()

	// pre-warm the system. we don't care about what happens with this
	// request, but we want Mixer, etc., to be ready to go when the actual
	// Tests start.
	if err = visitProductPage(60*time.Second, 200); err != nil {
		glog.Infof("initial product page request failed: %v", err)
	}

	allowPrometheusSync()

	return
}

func createDefaultRoutingRules() error {
	if err := createRouteRule(routeAllRule); err != nil {
		return fmt.Errorf("could not create base routing rules: %v", err)
	}
	allowRuleSync()
	return nil
}

func (t *testConfig) Teardown() error {
	return deleteDefaultRoutingRules()
}

func deleteDefaultRoutingRules() error {
	if err := deleteRouteRule(routeAllRule); err != nil {
		return fmt.Errorf("could not delete default routing rule: %v", err)
	}
	return nil
}

type promProxy struct {
	namespace      string
	portFwdProcess *os.Process
}

func newPromProxy(namespace string) *promProxy {
	return &promProxy{
		namespace: namespace,
	}
}

func dumpK8Env() {
	_, _ = util.Shell("kubectl --namespace %s get pods -o wide", tc.Kube.Namespace)

	podLogs("istio=ingress", "istio-ingress")
	podLogs("istio=mixer", "mixer")
	podLogs("istio=pilot", "discovery")
	podLogs("app=productpage", "istio-proxy")

}

func podID(labelSelector string) (pod string, err error) {
	pod, err = util.Shell("kubectl -n %s get pod -l %s -o jsonpath='{.items[0].metadata.name}'", tc.Kube.Namespace, labelSelector)
	if err != nil {
		glog.Warningf("could not get %s pod: %v", labelSelector, err)
		return
	}
	pod = strings.Trim(pod, "'")
	glog.Infof("%s pod name: %s", labelSelector, pod)
	return
}

func podLogs(labelSelector string, container string) {
	pod, err := podID(labelSelector)
	if err != nil {
		return
	}
	str, err := util.Shell("kubectl --namespace %s logs %s -c %s --tail=40 -p", tc.Kube.Namespace, pod, container)
	// This command can result in the following error.
	// Error from server (BadRequest): previous terminated container "istio-ingress" in pod "istio-ingress-4149475009-xqhzh" not found
	// "-p" tries to fetch logs for a previously dead container, however if no container has died, the command returns an error.
	if err != nil && !strings.Contains(err.Error(), "previous terminated container") {
		glog.Warningf("error: %s\n%v", str, err)
	}
	str, err = util.Shell("kubectl --namespace %s logs %s -c %s --tail=40", tc.Kube.Namespace, pod, container)
	if err != nil {
		glog.Warningf("error: %s\n%v", str, err)
	}
}

// portForward sets up local port forward to the pod specified by the "app" label
func (p *promProxy) portForward(labelSelector string, localPort string, remotePort string) error {
	var pod string
	var err error

	getName := fmt.Sprintf("kubectl -n %s get pod -l %s -o jsonpath='{.items[0].metadata.name}'", p.namespace, labelSelector)
	pod, err = util.Shell(getName)
	if err != nil {
		return err
	}
	glog.Infof("%s pod name: %s", labelSelector, pod)

	glog.Infof("Setting up %s proxy", labelSelector)
	portFwdCmd := fmt.Sprintf("kubectl port-forward %s %s:%s -n %s", strings.Trim(pod, "'"), localPort, remotePort, p.namespace)
	glog.Info(portFwdCmd)
	if p.portFwdProcess, err = util.RunBackground(portFwdCmd); err != nil {
		glog.Errorf("Failed to port forward: %s", err)
		return err
	}
	glog.Infof("running %s port-forward in background, pid = %d", labelSelector, p.portFwdProcess.Pid)
	return nil
}

func (p *promProxy) Setup() error {
	var err error

	if err = p.portForward("app=prometheus", prometheusPort, prometheusPort); err != nil {
		return err
	}

	if err = p.portForward("istio=mixer", mixerMetricsPort, mixerMetricsPort); err != nil {
		return err
	}

	if err = p.portForward("app=productpage", productPagePort, "9080"); err != nil {
		return err
	}

	return nil
}

func (p *promProxy) Teardown() (err error) {
	glog.Info("Cleaning up mixer proxy")
	if p.portFwdProcess != nil {
		err := p.portFwdProcess.Kill()
		if err != nil {
			glog.Errorf("Failed to kill port-forward process, pid: %d", p.portFwdProcess.Pid)
		}
	}
	return
}
func TestMain(m *testing.M) {
	flag.Parse()
	check(framework.InitGlog(), "cannot setup glog")
	check(setTestConfig(), "could not create TestConfig")
	tc.Cleanup.RegisterCleanable(tc)
	os.Exit(tc.RunTest(m))
}

func fatalf(t *testing.T, format string, args ...interface{}) {
	dumpK8Env()
	t.Fatalf(format, args...)
}

func errorf(t *testing.T, format string, args ...interface{}) {
	dumpK8Env()
	t.Errorf(format, args...)
}

func TestGlobalCheckAndReport(t *testing.T) {
	// setup prometheus API
	promAPI, err := promAPI()
	if err != nil {
		t.Fatalf("Could not build prometheus API client: %v", err)
	}

	// establish baseline
	t.Log("Establishing metrics baseline for test...")
	query := fmt.Sprintf("request_count{%s=\"%s\"}", destLabel, fqdn("productpage"))
	t.Logf("prometheus query: %s", query)
	value, err := promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		t.Fatalf("Could not get metrics from prometheus: %v", err)
	}

	prior200s, err := vectorValue(value, map[string]string{responseCodeLabel: "200"})
	if err != nil {
		t.Logf("error getting prior 200s, using 0 as value (msg: %v)", err)
		prior200s = 0
	}

	t.Logf("Baseline established: prior200s = %f", prior200s)
	t.Log("Visiting product page...")

	if errNew := visitProductPage(productPageTimeout, http.StatusOK); errNew != nil {
		t.Fatalf("Test app setup failure: %v", errNew)
	}
	allowPrometheusSync()

	glog.Info("Successfully sent request(s) to /productpage; checking metrics...")

	query = fmt.Sprintf("request_count{%s=\"%s\",%s=\"200\"}", destLabel, fqdn("productpage"), responseCodeLabel)
	t.Logf("prometheus query: %s", query)
	value, err = promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		fatalf(t, "Could not get metrics from prometheus: %v", err)
	}
	glog.Infof("promvalue := %s", value.String())

	got, err := vectorValue(value, map[string]string{})
	if err != nil {
		t.Logf("prometheus values for request_count:\n%s", promDump(promAPI, "request_count"))
		fatalf(t, "Could not find metric value: %v", err)
	}
	t.Logf("Got request_count (200s) of: %f", got)
	t.Logf("Actual new requests observed: %f", got-prior200s)

	want := float64(1)
	if (got - prior200s) < want {
		t.Logf("prometheus values for request_count:\n%s", promDump(promAPI, "request_count"))
		errorf(t, "Bad metric value: got %f, want at least %f", got-prior200s, want)
	}
}

func TestTcpMetrics(t *testing.T) {
	if err := replaceRouteRule(tcpDbRule); err != nil {
		t.Fatalf("Could not update reviews routing rule: %v", err)
	}
	defer func() {
		if err := deleteRouteRule(tcpDbRule); err != nil {
			t.Fatalf("Could not delete reviews routing rule: %v", err)
		}
	}()
	allowRuleSync()

	if err := visitProductPage(productPageTimeout, http.StatusOK); err != nil {
		t.Fatalf("Test app setup failure: %v", err)
	}
	allowPrometheusSync()

	glog.Info("Successfully sent request(s) to /productpage; checking metrics...")

	promAPI, err := promAPI()
	if err != nil {
		fatalf(t, "Could not build prometheus API client: %v", err)
	}
	query := fmt.Sprintf("tcp_bytes_sent{destination_service=\"%s\"}", fqdn("mongodb"))
	t.Logf("prometheus query: %s", query)
	value, err := promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		fatalf(t, "Could not get metrics from prometheus: %v", err)
	}
	glog.Infof("promvalue := %s", value.String())

	got, err := vectorValue(value, map[string]string{})
	if err != nil {
		t.Logf("prometheus values for tcp_bytes_sent:\n%s", promDump(promAPI, "tcp_bytes_sent"))
		fatalf(t, "Could not find metric value: %v", err)
	}
	t.Logf("tcp_bytes_sent: %f", got)
	want := float64(1)
	if got < want {
		t.Logf("prometheus values for tcp_bytes_sent:\n%s", promDump(promAPI, "tcp_bytes_sent"))
		errorf(t, "Bad metric value: got %f, want at least %f", got, want)
	}

	query = fmt.Sprintf("tcp_bytes_received{destination_service=\"%s\"}", fqdn("mongodb"))
	t.Logf("prometheus query: %s", query)
	value, err = promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		fatalf(t, "Could not get metrics from prometheus: %v", err)
	}
	glog.Infof("promvalue := %s", value.String())

	got, err = vectorValue(value, map[string]string{})
	if err != nil {
		t.Logf("prometheus values for tcp_bytes_received:\n%s", promDump(promAPI, "tcp_bytes_received"))
		fatalf(t, "Could not find metric value: %v", err)
	}
	t.Logf("tcp_bytes_received: %f", got)
	if got < want {
		t.Logf("prometheus values for tcp_bytes_received:\n%s", promDump(promAPI, "tcp_bytes_received"))
		errorf(t, "Bad metric value: got %f, want at least %f", got, want)
	}
}

func TestNewMetrics(t *testing.T) {
	if err := applyMixerRule(newTelemetryRule); err != nil {
		fatalf(t, "could not create required mixer rule: %v", err)
	}

	defer func() {
		if err := deleteMixerRule(newTelemetryRule); err != nil {
			t.Logf("could not clear rule: %v", err)
		}
	}()

	dumpK8Env()
	allowRuleSync()

	if err := visitProductPage(productPageTimeout, http.StatusOK); err != nil {
		fatalf(t, "Test app setup failure: %v", err)
	}

	glog.Info("Successfully sent request(s) to /productpage; checking metrics...")
	allowPrometheusSync()
	promAPI, err := promAPI()
	if err != nil {
		fatalf(t, "Could not build prometheus API client: %v", err)
	}
	query := fmt.Sprintf("response_size_count{%s=\"%s\",%s=\"200\"}", destLabel, fqdn("productpage"), responseCodeLabel)
	t.Logf("prometheus query: %s", query)
	value, err := promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		fatalf(t, "Could not get metrics from prometheus: %v", err)
	}
	glog.Infof("promvalue := %s", value.String())

	got, err := vectorValue(value, map[string]string{})
	if err != nil {
		t.Logf("prometheus values for response_size_count:\n%s", promDump(promAPI, "response_size_count"))
		t.Logf("prometheus values for request_count:\n%s", promDump(promAPI, "request_count"))
		fatalf(t, "Could not find metric value: %v", err)
	}
	want := float64(1)
	if got < want {
		t.Logf("prometheus values for response_size_count:\n%s", promDump(promAPI, "response_size_count"))
		t.Logf("prometheus values for request_count:\n%s", promDump(promAPI, "request_count"))
		errorf(t, "Bad metric value: got %f, want at least %f", got, want)
	}
}

func TestDenials(t *testing.T) {
	if err := visitProductPage(productPageTimeout, http.StatusOK); err != nil {
		fatalf(t, "Test app setup failure: %v", err)
	}

	// deny rule will deny all requests to product page unless
	// ["x-user"] header is set.
	glog.Infof("Denials: block productpage if x-user header is missing")
	if err := applyMixerRule(denialRule); err != nil {
		fatalf(t, "could not create required mixer rule: %v", err)
	}

	defer func() {
		if err := deleteMixerRule(denialRule); err != nil {
			t.Logf("could not clear rule: %v", err)
		}
	}()

	time.Sleep(10 * time.Second)

	// Product page should not be accessible anymore.
	glog.Infof("Denials: ensure productpage is denied access")
	if err := visitProductPage(productPageTimeout, http.StatusForbidden, &header{"x-user", ""}); err != nil {
		fatalf(t, "product page was not denied: %v", err)
	}

	// Product page *should be* accessible with x-user header.
	glog.Infof("Denials: ensure productpage is accessible for testuser")
	if err := visitProductPage(productPageTimeout, http.StatusOK, &header{"x-user", "testuser"}); err != nil {
		fatalf(t, "product page was not denied: %v", err)
	}
}

func TestRateLimit(t *testing.T) {
	if err := replaceRouteRule(routeReviewsV3Rule); err != nil {
		fatalf(t, "Could not create replace reviews routing rule: %v", err)
	}

	// the rate limit rule applies a max rate limit of 1 rps to the ratings service.
	if err := applyMixerRule(rateLimitRule); err != nil {
		fatalf(t, "could not create required mixer rule: %v", err)
	}
	defer func() {
		if err := deleteMixerRule(rateLimitRule); err != nil {
			t.Logf("could not clear rule: %v", err)
		}
	}()

	allowRuleSync()

	// setup prometheus API
	promAPI, err := promAPI()
	if err != nil {
		fatalf(t, "Could not build prometheus API client: %v", err)
	}

	// establish baseline
	t.Log("Establishing metrics baseline for test...")
	query := fmt.Sprintf("request_count{%s=\"%s\"}", destLabel, fqdn("ratings"))
	t.Logf("prometheus query: %s", query)
	value, err := promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		fatalf(t, "Could not get metrics from prometheus: %v", err)
	}

	prior429s, err := vectorValue(value, map[string]string{responseCodeLabel: "429"})
	if err != nil {
		t.Logf("error getting prior 429s, using 0 as value (msg: %v)", err)
		prior429s = 0
	}

	prior200s, err := vectorValue(value, map[string]string{responseCodeLabel: "200"})
	if err != nil {
		t.Logf("error getting prior 200s, using 0 as value (msg: %v)", err)
		prior200s = 0
	}
	t.Logf("Baseline established: prior200s = %f, prior429s = %f", prior200s, prior429s)

	t.Log("Sending traffic...")

	url := fmt.Sprintf("%s/productpage", tc.gateway)

	// run at a large QPS (here 100) for a minute to ensure that enough
	// traffic is generated to trigger 429s from the rate limit rule
	opts := fortio.HTTPRunnerOptions{
		RunnerOptions: fortio.RunnerOptions{
			QPS:        10,
			Duration:   1 * time.Minute,
			NumThreads: 8,
		},
		URL: url,
	}

	// productpage should still return 200s when ratings is rate-limited.
	res, err := fortio.RunHTTPTest(&opts)
	if err != nil {
		fatalf(t, "Generating traffic via fortio failed: %v", err)
	}

	allowPrometheusSync()

	totalReqs := res.DurationHistogram.Count
	succReqs := float64(res.RetCodes[http.StatusOK])
	badReqs := res.RetCodes[http.StatusBadRequest]

	glog.Info("Successfully sent request(s) to /productpage; checking metrics...")
	t.Logf("Fortio Summary: %d reqs (%f 200s (%f rps), %d 400s)", totalReqs, succReqs, succReqs/opts.Duration.Seconds(), badReqs)

	// consider only successful requests (as recorded at productpage service)
	callsToRatings := succReqs

	// the rate-limit is 1 rps
	want200s := opts.Duration.Seconds()

	// everything in excess of 200s should be 429s (ideally)
	want429s := callsToRatings - want200s

	t.Logf("Expected Totals: 200s: %f (%f rps), 429s: %f (%f rps)", want200s, want200s/opts.Duration.Seconds(), want429s, want429s/opts.Duration.Seconds())

	// if we received less traffic than the expected enforced limit to ratings
	// then there is no way to determine if the rate limit was applied at all
	// and for how much traffic. log all metrics and abort test.
	if callsToRatings < want200s {
		t.Logf("full set of prometheus metrics:\n%s", promDump(promAPI, "request_count"))
		fatalf(t, "Not enough traffic generated to exercise rate limit: ratings_reqs=%f, want200s=%f", callsToRatings, want200s)
	}

	query = fmt.Sprintf("request_count{%s=\"%s\"}", destLabel, fqdn("ratings"))
	t.Logf("prometheus query: %s", query)
	value, err = promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		fatalf(t, "Could not get metrics from prometheus: %v", err)
	}
	glog.Infof("promvalue := %s", value.String())

	got, err := vectorValue(value, map[string]string{responseCodeLabel: "429", "destination_version": "v1"})
	if err != nil {
		t.Logf("prometheus values for request_count:\n%s", promDump(promAPI, "request_count"))
		fatalf(t, "Could not find rate limit value: %v", err)
	}

	// establish some baseline to protect against flakiness due to randomness in routing
	want := math.Floor(want429s * .75)

	got = got - prior429s

	t.Logf("Actual 429s: %f (%f rps)", got, got/opts.Duration.Seconds())

	// check resource exhausteds
	if got < want {
		t.Logf("prometheus values for request_count:\n%s", promDump(promAPI, "request_count"))
		errorf(t, "Bad metric value for rate-limited requests (429s): got %f, want at least %f", got, want)
	}

	got, err = vectorValue(value, map[string]string{responseCodeLabel: "200", "destination_version": "v1"})
	if err != nil {
		t.Logf("prometheus values for request_count:\n%s", promDump(promAPI, "request_count"))
		fatalf(t, "Could not find successes value: %v", err)
	}

	got = got - prior200s

	t.Logf("Actual 200s: %f (%f rps)", got, got/opts.Duration.Seconds())

	// establish some baseline to protect against flakiness due to randomness in routing
	// and to allow for leniency in actual ceiling of enforcement (if 10 is the limit, but we allow slightly
	// less than 10, don't fail this test).
	want = math.Floor(want200s * .5)

	// check successes
	if got < want {
		t.Logf("prometheus values for request_count:\n%s", promDump(promAPI, "request_count"))
		errorf(t, "Bad metric value for successful requests (200s): got %f, want at least %f", got, want)
	}

	if got > want200s {
		t.Logf("prometheus values for request_count:\n%s", promDump(promAPI, "request_count"))
		errorf(t, "Bad metric value for successful requests (200s): got %f, want at most %f", got, want200s)
	}
}

func allowRuleSync() {
	glog.Info("Sleeping to allow rules to take effect...")
	time.Sleep(1 * time.Minute)
}

func allowPrometheusSync() {
	glog.Info("Sleeping to allow prometheus to record metrics...")
	time.Sleep(30 * time.Second)
}

func promAPI() (v1.API, error) {
	client, err := api.NewClient(api.Config{Address: fmt.Sprintf("http://localhost:%s", prometheusPort)})
	if err != nil {
		return nil, err
	}
	return v1.NewAPI(client), nil
}

// promDump gets all of the recorded values for a metric by name and generates a report of the values.
// used for debugging of failures to provide a comprehensive view of traffic experienced.
func promDump(client v1.API, metric string) string {
	if value, err := client.Query(context.Background(), fmt.Sprintf("%s{}", metric), time.Now()); err == nil {
		return value.String()
	}
	return ""
}

func vectorValue(val model.Value, labels map[string]string) (float64, error) {
	if val.Type() != model.ValVector {
		return 0, fmt.Errorf("value not a model.Vector; was %s", val.Type().String())
	}

	value := val.(model.Vector)
	for _, sample := range value {
		metric := sample.Metric
		nameCount := len(labels)
		for k, v := range metric {
			if labelVal, ok := labels[string(k)]; ok && labelVal == string(v) {
				nameCount--
			}
		}
		if nameCount == 0 {
			return float64(sample.Value), nil
		}
	}
	return 0, fmt.Errorf("value not found for %#v", labels)
}

// checkProductPageDirect
func checkProductPageDirect() {
	glog.Info("checkProductPageDirect")
	dumpURL("http://localhost:"+productPagePort+"/productpage", false)
}

// dumpMixerMetrics fetch metrics directly from mixer and dump them
func dumpMixerMetrics() {
	glog.Info("dumpMixerMetrics")
	dumpURL("http://localhost:"+mixerMetricsPort+"/metrics", true)
}

func dumpURL(url string, dumpContents bool) {
	clnt := &http.Client{
		Timeout: 1 * time.Minute,
	}
	status, contents, err := get(clnt, url)
	glog.Infof("%s ==> %d, <%v>", url, status, err)
	if dumpContents {
		glog.Infoln(contents)
	}
}

type header struct {
	name  string
	value string
}

func get(clnt *http.Client, url string, headers ...*header) (status int, contents string, err error) {
	var req *http.Request
	req, err = http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, "", err
	}

	for _, hdr := range headers {
		req.Header.Set(hdr.name, hdr.value)
	}
	resp, err := clnt.Do(req)
	if err != nil {
		glog.Warningf("Error communicating with %s: %v", url, err)
	} else {
		glog.Infof("Get from %s: %s (%d)", url, resp.Status, resp.StatusCode)
		var ba []byte
		ba, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			glog.Warningf("Unable to connect to read from %s: %v", url, err)
			return
		}
		contents = string(ba)
		status = resp.StatusCode
		closeResponseBody(resp)
	}
	return
}

func visitProductPage(timeout time.Duration, wantStatus int, headers ...*header) error {
	start := time.Now()
	clnt := &http.Client{
		Timeout: 1 * time.Minute,
	}
	url := tc.gateway + "/productpage"

	for {
		status, _, err := get(clnt, url, headers...)
		if err != nil {
			glog.Warningf("Unable to connect to product page: %v", err)
		}

		if status == wantStatus {
			glog.Infof("Got %d response from product page!", wantStatus)
			return nil
		}

		if time.Since(start) > timeout {
			dumpMixerMetrics()
			checkProductPageDirect()
			return fmt.Errorf("could not retrieve product page in %v: Last status: %v", timeout, status)
		}

		// see what is happening
		dumpK8Env()

		time.Sleep(3 * time.Second)
	}
}

func fqdn(service string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", service, tc.Kube.Namespace)
}

func createRouteRule(ruleName string) error {
	rule := filepath.Join(tc.rulesDir, ruleName)
	return util.KubeApply(tc.Kube.Namespace, rule)
}

func replaceRouteRule(ruleName string) error {
	rule := filepath.Join(tc.rulesDir, ruleName)
	return util.KubeApply(tc.Kube.Namespace, rule)
}

func deleteRouteRule(ruleName string) error {
	rule := filepath.Join(tc.rulesDir, ruleName)
	return util.KubeDelete(tc.Kube.Namespace, rule)
}

func deleteMixerRule(ruleName string) error {
	return doMixerRule(ruleName, util.KubeDeleteContents)
}

func applyMixerRule(ruleName string) error {
	return doMixerRule(ruleName, util.KubeApplyContents)
}

type kubeDo func(namespace string, contents string) error

// doMixerRule
// New mixer rules contain fully qualified pointers to other
// resources, they must be replaced by the current namespace.
func doMixerRule(ruleName string, do kubeDo) error {
	rule := filepath.Join(tc.rulesDir, ruleName)
	cb, err := ioutil.ReadFile(rule)
	if err != nil {
		glog.Errorf("Cannot read original yaml file %s", rule)
		return err
	}
	contents := string(cb)
	if !strings.Contains(contents, templateNamespace) {
		return fmt.Errorf("%s must contain %s so the it can replaced", rule, templateNamespace)
	}
	contents = strings.Replace(contents, templateNamespace, tc.Kube.Namespace, -1)
	return do(tc.Kube.Namespace, contents)
}

func setTestConfig() error {
	cc, err := framework.NewCommonConfig("mixer_test")
	if err != nil {
		return err
	}
	tc = new(testConfig)
	tc.CommonConfig = cc
	tmpDir, err := ioutil.TempDir(os.TempDir(), "mixer_test")
	if err != nil {
		return err
	}
	tc.rulesDir = tmpDir
	demoApps := []framework.App{
		{
			AppYaml:    util.GetResourcePath(bookinfoYaml),
			KubeInject: true,
		},
		{
			AppYaml:    util.GetResourcePath(bookinfoRatingsv2Yaml),
			KubeInject: true,
		},
		{
			AppYaml:    util.GetResourcePath(bookinfoDbYaml),
			KubeInject: true,
		},
	}
	for i := range demoApps {
		tc.Kube.AppManager.AddApp(&demoApps[i])
	}
	mp := newPromProxy(tc.Kube.Namespace)
	tc.Cleanup.RegisterCleanable(mp)
	return nil
}

func check(err error, msg string) {
	if err != nil {
		glog.Fatalf("%s. Error %s", msg, err)
	}
}

func closeResponseBody(r *http.Response) {
	if err := r.Body.Close(); err != nil {
		glog.Error(err)
	}
}
