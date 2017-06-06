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
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang/glog"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/e2e/util"
)

const (
	bookinfoYaml             = "samples/apps/bookinfo/bookinfo.yaml"
	rulesDir                 = "samples/apps/bookinfo"
	rateLimitRule            = "mixer-rule-ratings-ratelimit.yaml"
	denialRule               = "mixer-rule-ratings-denial.yaml"
	newTelemetryRule         = "mixer-rule-additional-telemetry.yaml"
	routeAllRule             = "route-rule-all-v1.yaml"
	routeReviewsVersionsRule = "route-rule-reviews-v2-v3.yaml"
	emptyRule                = "mixer-rule-empty-rule.yaml"
	luaTemplate              = "tests/e2e/tests/testdata/sample.lua.template"

	mixerPrometheusPort = 42422

	targetLabel       = "target"
	responseCodeLabel = "response_code"

	global = "global"
)

type testConfig struct {
	*framework.CommonConfig
	gateway  string
	rulesDir string
	wrk      *util.Wrk
}

// Template for ../test/testdata/sample.lua.template
type luaScriptTemplate struct {
	JSONFile  string
	ErrorFile string
}

// SampleResponseJSON for ../test/testdata/sample.lua.template
type luaScriptResponseJSON struct {
	CompletedRequests int64   `json:"completedRequests"`
	FailedRequests    int64   `json:"failedRequests"`
	TimeoutRequests   int64   `json:"timeoutRequests"`
	Non2xxResponses   int64   `json:"non2xxResponses"`
	KiloBytesPerSec   float64 `json:"KiloBytesPerSec"`
	MaxLatencyMs      float64 `json:"maxLatencyMs"`
	MeanLatencyMs     float64 `json:"meanLatencyMs"`
	P50LatencyMs      float64 `json:"p50LatencyMs"`
	P66LatencyMs      float64 `json:"p66LatencyMs"`
	P75LatencyMs      float64 `json:"p75LatencyMs"`
	P80LatencyMs      float64 `json:"p80LatencyMs"`
	P90LatencyMs      float64 `json:"p90LatencyMs"`
	P95LatencyMs      float64 `json:"p95LatencyMs"`
	P98LatencyMs      float64 `json:"p98LatencyMs"`
	P99LatencyMs      float64 `json:"p99LatencyMs"`
	RequestsPerSecond float64 `json:"requestsPerSecond"`
}

var (
	tc    *testConfig
	rules = []string{rateLimitRule, denialRule, newTelemetryRule, routeAllRule, routeReviewsVersionsRule, emptyRule}
)

func (t *testConfig) Setup() error {
	if err := t.wrk.Install(); err != nil {
		glog.Error("Failed to install wrk")
		return err
	}
	t.gateway = "http://" + tc.Kube.Ingress
	for _, rule := range rules {
		src := util.GetResourcePath(filepath.Join(rulesDir, fmt.Sprintf("%s", rule)))
		dest := filepath.Join(t.rulesDir, rule)
		srcBytes, err := ioutil.ReadFile(src)
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
	return nil
}
func (t *testConfig) Teardown() error { return nil }

type mixerProxy struct {
	namespace             string
	metricsPortFwdProcess *os.Process
	apiPortFwdProcess     *os.Process
}

func newMixerProxy(namespace string) *mixerProxy {
	return &mixerProxy{
		namespace: namespace,
	}
}

func (m *mixerProxy) Setup() error {
	var pod string
	var err error
	glog.Info("Setting up mixer proxy")
	getName := fmt.Sprintf("kubectl -n %s get pod -l istio=mixer -o jsonpath='{.items[0].metadata.name}'", m.namespace)
	pod, err = util.Shell(getName)
	if err != nil {
		return err
	}
	metricsPortFwd := fmt.Sprintf("kubectl port-forward %s %d:%d -n %s", strings.Trim(pod, "'"), mixerPrometheusPort, mixerPrometheusPort, m.namespace)
	if m.metricsPortFwdProcess, err = util.RunBackground(metricsPortFwd); err != nil {
		glog.Errorf("Failed to port forward: %s", err)
		return err
	}
	glog.Infof("mixer metrics port-forward running background, pid = %d", m.metricsPortFwdProcess.Pid)
	return err
}

func (m *mixerProxy) Teardown() (err error) {
	glog.Info("Cleaning up mixer proxy")
	if m.metricsPortFwdProcess != nil {
		err := m.metricsPortFwdProcess.Kill()
		if err != nil {
			glog.Error("Failed to kill metrics port-forward process, pid: %s", m.metricsPortFwdProcess.Pid)
		}
	}
	if m.apiPortFwdProcess != nil {
		err := m.apiPortFwdProcess.Kill()
		if err != nil {
			glog.Error("Failed to kill api port-forward process, pid: %s", m.apiPortFwdProcess.Pid)
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

func TestGlobalCheckAndReport(t *testing.T) {
	if err := visitProductPage(5); err != nil {
		t.Fatalf("Test app setup failure: %v", err)
	}

	glog.Info("Successfully request to /productpage; checking Mixer status...")

	resp, err := getMixerMetrics()
	defer func() { glog.Infof("closed response: %v", resp.Body.Close()) }()
	if err != nil {
		t.Errorf("Could not get /metrics from mixer: %v", err)
		return
	}
	glog.Info("Successfully retrieved /metrics from mixer.")

	requests := &metricValueGetter{
		metricFamilyName: "request_count",
		labels:           map[string]string{targetLabel: fqdn("productpage"), responseCodeLabel: "200"},
		valueFn:          counterValue,
	}

	if err := metricValues(resp, requests); err != nil {
		t.Errorf("Error getting metrics: %got", err)
		return
	}
	if requests.value != 1 {
		t.Errorf("Bad metric value: got %f, want 1", requests.value)
	}
}

func TestNewMetrics(t *testing.T) {
	productpage := fqdn("productpage")
	if err := createMixerRule(global, productpage, newTelemetryRule); err != nil {
		t.Fatalf("could not create required mixer rule: %v", err)
	}
	defer func() {
		if err := createMixerRule(global, productpage, emptyRule); err != nil {
			t.Logf("could not clear rule: %v", err)
		}
	}()

	// allow time for configuration to go and be active.
	// TODO: figure out a better way to confirm rule active
	time.Sleep(5 * time.Second)

	if err := visitProductPage(5); err != nil {
		t.Fatalf("Test app setup failure: %v", err)
	}

	glog.Info("Successfully request to /productpage; checking Mixer status...")

	resp, err := getMixerMetrics()
	defer func() { glog.Infof("closed response: %v", resp.Body.Close()) }()
	if err != nil {
		t.Errorf("Could not get /metrics from mixer: %v", err)
		return
	}
	glog.Info("Successfully retrieved /metrics from mixer.")
	responses := &metricValueGetter{
		metricFamilyName: "response_size",
		labels:           map[string]string{targetLabel: fqdn("productpage"), responseCodeLabel: "200"},
		valueFn:          histogramCountValue,
	}

	if err := metricValues(resp, responses); err != nil {
		t.Errorf("Error getting metrics: %got", err)
		return
	}
	if responses.value != 1 {
		t.Errorf("Bad metric value: got %f, want 1", responses.value)
	}
}

func TestDenials(t *testing.T) {
	t.Skip("REGRESSION. Skipping until fixed.")
	applyReviewsRoutingRules(t)
	defer func() {
		deleteReviewsRoutingRules(t)
	}()

	ratings := fqdn("ratings")
	if err := createMixerRule(global, ratings, denialRule); err != nil {
		t.Fatalf("Could not create required mixer rule: %v", err)
	}
	defer func() {
		if err := createMixerRule(global, ratings, emptyRule); err != nil {
			t.Logf("could not clear rule: %v", err)
		}
	}()

	// allow time for configuration to go and be active.
	// TODO: figure out a better way to confirm rule active
	time.Sleep(5 * time.Second)

	// generate several calls to the product page
	for i := 0; i < 6; i++ {
		if err := visitProductPage(5); err != nil {
			t.Fatalf("Test app setup failure: %v", err)
		}
	}

	glog.Info("Successfully request to /productpage; checking Mixer status...")

	resp, err := getMixerMetrics()
	defer func() { glog.Infof("closed response: %v", resp.Body.Close()) }()
	if err != nil {
		t.Errorf("Could not get /metrics from mixer: %v", err)
		return
	}
	glog.Info("Successfully retrieved /metrics from mixer.")
	requests := &metricValueGetter{
		metricFamilyName: "request_count",
		labels: map[string]string{
			targetLabel:       fqdn("ratings"),
			responseCodeLabel: "400",
		},
		valueFn: counterValue,
	}

	if err := metricValues(resp, requests); err != nil {
		t.Errorf("Error getting metrics: %got", err)
		return
	}
	// be liberal in search for denial
	if requests.value < 1 {
		t.Errorf("Bad metric value: got %f, want at least 1", requests.value)
	}
}

func TestRateLimit(t *testing.T) {
	applyReviewsRoutingRules(t)
	defer func() {
		deleteReviewsRoutingRules(t)
	}()

	ratings := fqdn("ratings")
	if err := createMixerRule(global, ratings, rateLimitRule); err != nil {
		t.Fatalf("Could not create required mixer rule: %got", err)
	}
	defer func() {
		if err := createMixerRule(global, ratings, emptyRule); err != nil {
			t.Logf("could not clear rule: %got", err)
		}
	}()

	// allow time for configuration to go and be active.
	// TODO: figure out a better way to confirm rule active
	time.Sleep(1 * time.Minute)
	threads := 5
	timeout := "2m"
	connections := 5
	duration := "2m"
	errorFile := filepath.Join(tc.Kube.TmpDir, "TestRateLimit.err")
	JSONFile := filepath.Join(tc.Kube.TmpDir, "TestRateLimit.Json")
	url := fmt.Sprintf("%s/productpage", tc.gateway)

	luaScript := &util.LuaTemplate{
		TemplatePath: util.GetResourcePath(luaTemplate),
		Template: &luaScriptTemplate{
			ErrorFile: errorFile,
			JSONFile:  JSONFile,
		},
	}
	if err := luaScript.Generate(); err != nil {
		t.Errorf("Failed to generate lua script. %v", err)
		return
	}

	if err := tc.wrk.Run(
		"-t %d --timeout %s -c %d -d %s -s %s %s",
		threads, timeout, connections, duration, luaScript.Script, url); err != nil {
		t.Errorf("Failed to run wrk %v", err)
		return
	}

	// TODO: Use the JSON file to do more detailed checking
	r := &luaScriptResponseJSON{}
	if err := util.ReadJSON(JSONFile, r); err != nil {
		t.Errorf("Could not parse json. %v", err)
		return
	}

	glog.Info("Successfully request to /productpage; checking Mixer status...")

	resp, err := getMixerMetrics()
	defer func() { glog.Infof("closed response: %v", resp.Body.Close()) }()
	if err != nil {
		t.Errorf("Could not get /metrics from mixer: %got", err)
		return
	}
	glog.Info("Successfully retrieved /metrics from mixer.")
	rateLimitedReqs := &metricValueGetter{
		metricFamilyName: "request_count",
		labels: map[string]string{
			targetLabel:       fqdn("ratings"),
			responseCodeLabel: "429",
		},
		valueFn: counterValue,
	}

	okReqs := &metricValueGetter{
		metricFamilyName: "request_count",
		labels: map[string]string{
			targetLabel:       fqdn("ratings"),
			responseCodeLabel: "200",
		},
		valueFn: counterValue,
	}

	if err := metricValues(resp, rateLimitedReqs, okReqs); err != nil {
		t.Errorf("Error getting metrics: %got", err)
		return
	}
	// rate-limit set to 5 qps, sending a request every 1/10th of a second,
	// modulo goroutine scheduling, etc., with slightly less than 7 qps going
	// to the rate-limited service. there should be ~100 429s observed.
	// check for greater than 60 to allow for differences in traffic generation, etc.
	if rateLimitedReqs.value < 60 {
		t.Errorf("Bad metric value: got %f, want at least 60", rateLimitedReqs.value)
	}

	// check to make sure that some requests were accepted and processed
	// successfully. there should be ~250 "OK" requests observed.
	if okReqs.value < 200 {
		t.Errorf("Bad metric value: got %f, want at least 200", okReqs.value)
	}
}

func applyReviewsRoutingRules(t *testing.T) {
	if err := createRouteRule(routeAllRule); err != nil {
		t.Fatalf("Could not create initial route-all rule: %v", err)
	}
	// hope for stability
	time.Sleep(30 * time.Second)
	if err := replaceRouteRule(routeReviewsVersionsRule); err != nil {
		t.Fatalf("Could not create replace reviews routing rule: %v", err)
	}
	// hope for stability
	time.Sleep(30 * time.Second)
}

func deleteReviewsRoutingRules(t *testing.T) {
	if err := deleteRouteRule(routeAllRule); err != nil {
		t.Fatalf("Could not delete initial route-all rule: %v", err)
	}
	// hope for stability
	time.Sleep(30 * time.Second)
}

func getMixerMetrics() (*http.Response, error) {
	// sleep for a small amount of time to allow for Report() processing to finish
	time.Sleep(1 * time.Minute)
	return http.Get("http://localhost:42422/metrics")
}

func visitProductPage(retries int) error {
	standby := 0
	for i := 0; i <= retries; i++ {
		time.Sleep(time.Duration(standby) * time.Second)
		http.DefaultClient.Timeout = 1 * time.Minute
		resp, err := http.Get(fmt.Sprintf("%s/productpage", tc.gateway))
		if err != nil {
			glog.Infof("Error talking to productpage: %s", err)
		} else {
			glog.Infof("Get from page: %d", resp.StatusCode)
			if resp.StatusCode == http.StatusOK {
				glog.Info("Get response from product page!")
				break
			}
			closeResponseBody(resp)
		}
		if i == retries {
			return errors.New("could not retrieve product page: default route Failed")
		}
		standby += 5
		glog.Errorf("Couldn't get to the bookinfo product page, trying again in %d second", standby)
	}
	if standby != 0 {
		time.Sleep(time.Minute)
	}
	return nil
}

func fqdn(service string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", service, tc.Kube.Namespace)
}

func createRouteRule(ruleName string) error {
	rule := filepath.Join(tc.rulesDir, ruleName)
	return tc.Kube.Istioctl.CreateRule(rule)
}

func replaceRouteRule(ruleName string) error {
	rule := filepath.Join(tc.rulesDir, ruleName)
	return tc.Kube.Istioctl.ReplaceRule(rule)
}

func deleteRouteRule(ruleName string) error {
	rule := filepath.Join(tc.rulesDir, ruleName)
	return tc.Kube.Istioctl.DeleteRule(rule)
}

func createMixerRule(scope, subject, ruleName string) error {
	rule := filepath.Join(tc.rulesDir, ruleName)
	return tc.Kube.Istioctl.CreateMixerRule(scope, subject, rule)
}

func counterValue(mf *dto.MetricFamily, name string, labels map[string]string) (float64, error) {
	if len(mf.Metric) == 0 {
		return 0, fmt.Errorf("no metrics in family %s: %#v", name, mf.Metric)
	}

	counterMetric, err := metric(mf, labels)
	if err != nil {
		return 0, err
	}
	counter := counterMetric.Counter
	if counter == nil {
		return 0, fmt.Errorf("expected non-nil counter for metric: %s", mf.GetName())
	}
	glog.Infof("%s: %#v", name, counter)
	return counter.GetValue(), nil
}

func histogramCountValue(mf *dto.MetricFamily, name string, labels map[string]string) (float64, error) {
	if len(mf.Metric) == 0 {
		return 0, fmt.Errorf("no metrics in family %s: %#v", name, mf.Metric)
	}
	hMetric, err := metric(mf, labels)
	if err != nil {
		return 0, err
	}
	histogram := hMetric.Histogram
	if histogram == nil {
		return 0, fmt.Errorf("expected non-nil histogram for metric: %s", mf.GetName())
	}
	glog.Infof("%s: %#v", name, histogram)
	return float64(histogram.GetSampleCount()), nil
}

func metricValues(resp *http.Response, getters ...*metricValueGetter) error {
	var buf bytes.Buffer
	tee := io.TeeReader(resp.Body, &buf)
	defer func() {
		glog.Infof("/metrics: \n%s", buf)
	}()

	decoder := expfmt.NewDecoder(tee, expfmt.ResponseFormat(resp.Header))

	// loop through metrics, looking to validate recorded request count
	for {
		mf := &dto.MetricFamily{}
		err := decoder.Decode(mf)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("could not get decode valueFn: %v", err)
		}
		for _, g := range getters {
			if g.applies(mf.GetName()) {
				if val, err := g.getMetricValue(mf); err == nil {
					g.value = val
				}
			}
		}
	}
}

func metric(mf *dto.MetricFamily, labels map[string]string) (*dto.Metric, error) {
	// range operations seem to cause hangs with metrics stuff
	// using indexed loops instead
	for i := 0; i < len(mf.GetMetric()); i++ {
		m := mf.Metric[i]
		nameCount := len(labels)
		for j := 0; j < len(m.Label); j++ {
			l := m.Label[j]
			name := l.GetName()
			val := l.GetValue()
			if v, ok := labels[name]; ok && v == val {
				nameCount--
			}
		}
		if nameCount == 0 {
			return m, nil
		}
	}
	return nil, fmt.Errorf("no metric found with labels: %#v", labels)
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
	tc.wrk = &util.Wrk{
		TmpDir: tmpDir,
	}
	demoApp := &framework.App{
		AppYaml:    util.GetResourcePath(bookinfoYaml),
		KubeInject: true,
	}
	tc.Kube.AppManager.AddApp(demoApp)
	mp := newMixerProxy(tc.Kube.Namespace)
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

type metricValueGetter struct {
	metricFamilyName string
	labels           map[string]string
	valueFn          metricValueFn
	value            float64
}

func (v metricValueGetter) applies(name string) bool {
	return v.metricFamilyName == name
}

func (v metricValueGetter) getMetricValue(mf *dto.MetricFamily) (float64, error) {
	return v.valueFn(mf, v.metricFamilyName, v.labels)
}

type metricValueFn func(mf *dto.MetricFamily, name string, labels map[string]string) (float64, error)
