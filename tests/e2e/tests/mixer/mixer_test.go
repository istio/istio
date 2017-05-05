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
	bookinfoYaml = "demos/apps/bookinfo/bookinfo.yaml"
	//rulesDir            = "demos/apps/bookinfo"
	//rateLimitRule       = "mixer-rule-ratings-ratelimit.yaml"
	//denialRule          = "mixer-rule-ratings-denial.yaml"
	newTelemetryRule = "mixer-rule-additional-telemetry.yaml"
	emptyRule        = "mixer-rule-empty-rule.yaml"

	mixerAPIPort        = 9094
	mixerPrometheusPort = 42422

	targetLabel       = "target"
	responseCodeLabel = "response_code"

	global = "global"
)

type testConfig struct {
	*framework.CommonConfig
	gateway  string
	rulesDir string
}

var tc *testConfig

func (t *testConfig) Setup() error {
	t.gateway = "http://" + tc.Kube.Ingress
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

	apiPortFwd := fmt.Sprintf("kubectl port-forward %s %d:%d -n %s", strings.Trim(pod, "'"), mixerAPIPort, mixerAPIPort, m.namespace)
	if m.apiPortFwdProcess, err = util.RunBackground(apiPortFwd); err != nil {
		glog.Errorf("Failed to port forward: %s", err)
		return err
	}
	glog.Infof("mixer config API port-forward running background, pid = %d", m.apiPortFwdProcess.Pid)

	err = os.Setenv("ISTIO_MIXER_API_SERVER", fmt.Sprintf("http://localhost:%d", mixerAPIPort))
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

	// sleep for a small amount of time to allow for Report() processing to finish
	time.Sleep(5 * time.Second)

	resp, err := http.Get("http://localhost:42422/metrics")
	defer resp.Body.Close()
	if err != nil {
		t.Errorf("Could not get /metrics from mixer: %v", err)
		return
	}
	glog.Info("Successfully retrieved /metrics from mixer.")

	requests := metricValueGetter{
		metricFamilyName: "request_count",
		labels:           map[string]string{targetLabel: fqdn("reviews"), responseCodeLabel: "200"},
		valueFn:          counterValue,
	}

	got, err := metricValue(resp, requests)
	if err != nil {
		t.Errorf("Error getting request count: %v", err)
		return
	}
	if got != 1 {
		t.Errorf("Bad request count: got %d, want 1", got)
	}
}

func TestMetricsTask(t *testing.T) {
	reviews := fqdn("reviews")
	if err := createMixerRule(global, reviews, newTelemetryRule); err != nil {
		t.Fatalf("could not create required mixer rule: %v", err)
	}
	defer func() {
		if err := createMixerRule(global, reviews, emptyRule); err != nil {
			t.Logf("could not clear rule: %v", err)
		}
	}()

	if err := visitProductPage(5); err != nil {
		t.Fatalf("Test app setup failure: %v", err)
	}

	glog.Info("Successfully request to /productpage; checking Mixer status...")

	// sleep for a small amount of time to allow for Report() processing to finish
	time.Sleep(5 * time.Second)

	resp, err := http.Get("http://localhost:42422/metrics")
	defer resp.Body.Close()
	if err != nil {
		t.Errorf("Could not get /metrics from mixer: %v", err)
		return
	}
	glog.Info("Successfully retrieved /metrics from mixer.")

	responses := metricValueGetter{
		metricFamilyName: "response_size",
		labels:           map[string]string{targetLabel: fqdn("reviews"), responseCodeLabel: "200"},
		valueFn:          histogramCountValue,
	}

	got, err := metricValue(resp, responses)
	if err != nil {
		t.Errorf("Error getting response_size: %v", err)
		return
	}
	if got != 1 {
		t.Errorf("Bad response_size_count: got %d, want 1", got)
	}
}

func visitProductPage(retries int) error {
	standby := 0
	for i := 0; i <= retries; i++ {
		time.Sleep(time.Duration(standby) * time.Second)
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
			return errors.New("Could not retrieve product page: default route Failed")
		}
		standby += 5
		glog.Errorf("Couldn't get to the bookinfo product page, trying again in %d second", standby)
	}
	return nil
}

func fqdn(service string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", service, tc.Kube.Namespace)
}

func createMixerRule(scope, subject, ruleName string) error {
	rule := filepath.Join(tc.rulesDir, ruleName)
	return tc.Kube.Istioctl.CreateMixerRule(scope, subject, rule)
}

func counterValue(mf *dto.MetricFamily, name string, labels map[string]string) (float64, error) {
	if len(mf.Metric) == 0 {
		return 0, fmt.Errorf("no metrics in family %s: %#v", name, mf.Metric)
	}
	counterMetric, err := metric(mf.Metric, labels)
	if err != nil {
		return 0, err
	}
	counter := counterMetric.Counter
	if counter == nil {
		return 0, fmt.Errorf("Expected non-nil counter for metric: %s", mf.GetName())
	}
	glog.Infof("%s: %#v", name, counter)
	return counter.GetValue(), nil
}

func histogramCountValue(mf *dto.MetricFamily, name string, labels map[string]string) (float64, error) {
	if len(mf.Metric) == 0 {
		return 0, fmt.Errorf("no metrics in family %s: %#v", name, mf.Metric)
	}
	hMetric, err := metric(mf.Metric, labels)
	if err != nil {
		return 0, err
	}
	histogram := hMetric.Histogram
	if histogram == nil {
		return 0, fmt.Errorf("Expected non-nil histogram for metric: %s", mf.GetName())
	}
	glog.Infof("%s: %#v", name, histogram)
	return float64(histogram.GetSampleCount()), nil
}

func metricValue(resp *http.Response, getter metricValueGetter) (float64, error) {
	decoder := expfmt.NewDecoder(resp.Body, expfmt.ResponseFormat(resp.Header))

	// loop through metrics, looking to validate recorded request count
	for {
		mf := &dto.MetricFamily{}
		err := decoder.Decode(mf)
		if err == io.EOF {
			return 0, errors.New("Expected to find request_count valueFn")
		}
		if err != nil {
			return 0, fmt.Errorf("Could not get decode valueFn: %v", err)
		}
		if getter.applies(mf.GetName()) {
			return getter.getMetricValue(mf)
		}
	}
}

func metric(metrics []*dto.Metric, labels map[string]string) (*dto.Metric, error) {
	// range operations seem to cause hangs with metrics stuff
	// using indexed loops instead
	for i := 0; i < len(metrics); i++ {
		m := metrics[i]
		nameCount := len(labels)
		for j := 0; j < len(m.Label); j++ {
			l := m.Label[j]
			name := l.GetName()
			val := l.GetValue()
			glog.Infof("metric label: %s = %s", name, val)
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
	tc.rulesDir, err = ioutil.TempDir(os.TempDir(), "mixer_test")
	if err != nil {
		return err
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
}

func (v metricValueGetter) applies(name string) bool {
	return v.metricFamilyName == name
}

func (v metricValueGetter) getMetricValue(mf *dto.MetricFamily) (float64, error) {
	return v.valueFn(mf, v.metricFamilyName, v.labels)
}

type metricValueFn func(mf *dto.MetricFamily, name string, labels map[string]string) (float64, error)
