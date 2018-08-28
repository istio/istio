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
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"fortio.org/fortio/fhttp"

	// flog "fortio.org/fortio/log"
	"fortio.org/fortio/periodic"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	bookinfoSampleDir     = "samples/bookinfo"
	yamlExtension         = "yaml"
	deploymentDir         = "platform/kube"
	bookinfoYaml          = "bookinfo"
	bookinfoRatingsv2Yaml = "bookinfo-ratings-v2"
	bookinfoDbYaml        = "bookinfo-db"
	sleepYaml             = "samples/sleep/sleep"
	mixerTestDataDir      = "tests/e2e/tests/mixer/testdata"

	prometheusPort   = "9090"
	mixerMetricsPort = "42422"
	productPagePort  = "10000"

	srcLabel           = "source_service"
	srcPodLabel        = "source_pod"
	srcWorkloadLabel   = "source_workload"
	srcOwnerLabel      = "source_owner"
	srcUIDLabel        = "source_workload_uid"
	destLabel          = "destination_service"
	destPodLabel       = "destination_pod"
	destWorkloadLabel  = "destination_workload"
	destOwnerLabel     = "destination_owner"
	destUIDLabel       = "destination_workload_uid"
	destContainerLabel = "destination_container"
	responseCodeLabel  = "response_code"
	reporterLabel      = "reporter"

	// This namespace is used by default in all mixer config documents.
	// It will be replaced with the test namespace.
	templateNamespace = "istio-system"

	checkPath  = "/istio.mixer.v1.Mixer/Check"
	reportPath = "/istio.mixer.v1.Mixer/Report"

	testRetryTimes = 5

	redisInstallDir  = "stable/redis"
	redisInstallName = "redis-release"
)

type testConfig struct {
	*framework.CommonConfig
	rulesDir string
}

var (
	tc        *testConfig
	testFlags = &framework.TestFlags{
		Ingress: true,
		Egress:  true,
	}
	configVersion      = "v1alpha3"
	ingressName        = "ingressgateway"
	productPageTimeout = 60 * time.Second

	networkingDir               = "networking"
	policyDir                   = "policy"
	rateLimitRule               = "mixer-rule-ratings-ratelimit"
	denialRule                  = "mixer-rule-ratings-denial"
	ingressDenialRule           = "mixer-rule-ingress-denial"
	newTelemetryRule            = "mixer-rule-additional-telemetry"
	kubeenvTelemetryRule        = "mixer-rule-kubernetesenv-telemetry"
	destinationRuleAll          = "destination-rule-all"
	routeAllRule                = "virtual-service-all-v1"
	routeReviewsVersionsRule    = "virtual-service-reviews-v2-v3"
	routeReviewsV3Rule          = "virtual-service-reviews-v3"
	tcpDbRule                   = "virtual-service-ratings-db"
	bookinfoGateway             = "bookinfo-gateway"
	redisQuotaRollingWindowRule = "mixer-rule-ratings-redis-quota-rolling-window"
	redisQuotaFixedWindowRule   = "mixer-rule-ratings-redis-quota-fixed-window"

	defaultRules []string
	rules        []string
)

func init() {
	testFlags.Init()
}

// Setup is called from framework package init().
func (t *testConfig) Setup() (err error) {
	defer func() {
		if err != nil {
			dumpK8Env()
		}
	}()

	drs := []*string{&bookinfoGateway, &destinationRuleAll, &routeAllRule}
	for _, dr := range drs {
		*dr = filepath.Join(bookinfoSampleDir, networkingDir, *dr)
		defaultRules = append(defaultRules, *dr)
	}

	rs := []*string{&rateLimitRule, &denialRule, &ingressDenialRule, &newTelemetryRule,
		&kubeenvTelemetryRule}
	for _, r := range rs {
		*r = filepath.Join(bookinfoSampleDir, policyDir, *r)
		rules = append(rules, *r)
	}

	rs = []*string{&routeReviewsVersionsRule, &routeReviewsV3Rule, &tcpDbRule}
	for _, r := range rs {
		*r = filepath.Join(bookinfoSampleDir, networkingDir, *r)
		rules = append(rules, *r)
	}

	rs = []*string{&redisQuotaRollingWindowRule, &redisQuotaFixedWindowRule}
	for _, r := range rs {
		*r = filepath.Join(mixerTestDataDir, *r)
		rules = append(rules, *r)
	}

	log.Infof("new rule %s", rateLimitRule)
	log.Infof("Rules are default: %v, test-specific: %v", defaultRules, rules)
	for _, rule := range append(defaultRules, rules...) {
		err = copyRuleToFilesystem(t, rule)
		if err != nil {
			return nil
		}
	}

	if !util.CheckPodsRunning(tc.Kube.Namespace, tc.Kube.KubeConfig) {
		return fmt.Errorf("can't get all pods running")
	}

	if err = setupDefaultRouting(); err != nil {
		return err
	}
	allowRuleSync()

	// pre-warm the system. we don't care about what happens with this
	// request, but we want Mixer, etc., to be ready to go when the actual
	// Tests start.
	if err = visitProductPage(30*time.Second, 200); err != nil {
		log.Infof("initial product page request failed: %v", err)
	}

	allowPrometheusSync()

	return
}

func copyRuleToFilesystem(t *testConfig, rule string) error {
	src := getSourceRulePath(rule)
	dest := getDestinationRulePath(t, rule)
	log.Infof("Copying rule %s from %s to %s", rule, src, dest)
	ori, err := ioutil.ReadFile(src)
	if err != nil {
		log.Errorf("Failed to read original rule file %s", src)
		return err
	}
	content := string(ori)

	err = os.MkdirAll(filepath.Dir(dest), 0700)
	if err != nil {
		log.Errorf("Failed to create the directory %s", filepath.Dir(dest))
		return err
	}

	err = ioutil.WriteFile(dest, []byte(content), 0600)
	if err != nil {
		log.Errorf("Failed to write into new rule file %s", dest)
		return err
	}

	return nil
}

func getSourceRulePath(rule string) string {
	return util.GetResourcePath(filepath.Join(rule + "." + yamlExtension))
}

func getDestinationRulePath(t *testConfig, rule string) string {
	return filepath.Join(t.rulesDir, rule+"."+yamlExtension)
}

func setupDefaultRouting() error {
	log.Infof("setupDefaultRouting for %s", configVersion)
	if err := applyRules(defaultRules); err != nil {
		return fmt.Errorf("could not apply rules '%s': %v", defaultRules, err)
	}
	standby := 0
	for i := 0; i <= testRetryTimes; i++ {
		time.Sleep(time.Duration(standby) * time.Second)
		var gateway string
		var errGw error

		gateway, errGw = getIngressOrGateway()
		if errGw != nil {
			return errGw
		}

		resp, err := http.Get(fmt.Sprintf("%s/productpage", gateway))
		if err != nil {
			log.Infof("Error talking to productpage: %s", err)
		} else {
			log.Infof("Get from page: %d", resp.StatusCode)
			if resp.StatusCode == http.StatusOK {
				log.Info("Get response from product page!")
				break
			}
			closeResponseBody(resp)
		}
		if i == testRetryTimes {
			return errors.New("unable to set default route")
		}
		standby += 5
		log.Errorf("Couldn't get to the bookinfo product page, trying again in %d second", standby)
	}

	log.Info("Success! Default route got expected response")
	return nil
}

func applyRules(ruleKeys []string) error {
	for _, ruleKey := range ruleKeys {
		rule := getDestinationRulePath(tc, ruleKey)
		if err := util.KubeApply(tc.Kube.Namespace, rule, tc.Kube.KubeConfig); err != nil {
			//log.Errorf("Kubectl apply %s failed", rule)
			return err
		}
	}
	log.Info("Waiting for rules to propagate...")
	time.Sleep(time.Duration(30) * time.Second)
	return nil
}

func (t *testConfig) Teardown() error {
	return deleteAllRoutingConfig()
}

func deleteAllRoutingConfig() error {

	drs := []*string{&routeAllRule, &destinationRuleAll, &bookinfoGateway}

	var err error
	for _, dr := range drs {
		if err = deleteRoutingConfig(*dr); err != nil {
			log.Errorf("could not delete routing config: %v", err)
		}

	}

	return err
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

func dumpK8Env() {
	_, _ = util.Shell("kubectl --namespace %s get pods -o wide", tc.Kube.Namespace)

	podLogs("istio="+ingressName, ingressName)
	podLogs("istio=mixer", "mixer")
	podLogs("istio=pilot", "discovery")
	podLogs("app=productpage", "istio-proxy")

}

func podID(labelSelector string) (pod string, err error) {
	pod, err = util.Shell("kubectl -n %s get pod -l %s -o jsonpath='{.items[0].metadata.name}'", tc.Kube.Namespace, labelSelector)
	if err != nil {
		log.Warnf("could not get %s pod: %v", labelSelector, err)
		return
	}
	pod = strings.Trim(pod, "'")
	log.Infof("%s pod name: %s", labelSelector, pod)
	return
}

func deployment(labelSelector string) (name, owner, uid string, err error) {
	name, err = util.Shell("kubectl -n %s get deployment -l %s -o jsonpath='{.items[0].metadata.name}'", tc.Kube.Namespace, labelSelector)
	if err != nil {
		log.Warnf("could not get %s deployment: %v", labelSelector, err)
		return
	}
	log.Infof("%s deployment name: %s", labelSelector, name)
	uid = fmt.Sprintf("istio://%s/workloads/%s", tc.Kube.Namespace, name)
	selfLink, err := util.Shell("kubectl -n %s get deployment -l %s -o jsonpath='{.items[0].metadata.selfLink}'", tc.Kube.Namespace, labelSelector)
	if err != nil {
		log.Warnf("could not get deployment %s self link: %v", name, err)
		return
	}
	log.Infof("deployment %s self link: %s", labelSelector, selfLink)
	owner = fmt.Sprintf("kubernetes:/%s", selfLink)
	return
}

func podLogs(labelSelector string, container string) {
	pod, err := podID(labelSelector)
	if err != nil {
		return
	}
	log.Info("Expect and ignore an error getting crash logs when there are no crash (-p invocation)")
	_, _ = util.Shell("kubectl --namespace %s logs %s -c %s --tail=40 -p", tc.Kube.Namespace, pod, container)
	_, _ = util.Shell("kubectl --namespace %s logs %s -c %s --tail=40", tc.Kube.Namespace, pod, container)
}

// portForward sets up local port forward to the pod specified by the "app" label
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

	// Give it some time since process is launched in the background
	time.Sleep(3 * time.Second)
	if _, err = net.DialTimeout("tcp", ":"+localPort, 5*time.Second); err != nil {
		log.Errorf("Failed to port forward: %s", err)
		return err
	}

	log.Infof("running %s port-forward in background, pid = %d", labelSelector, proc.Pid)
	return nil
}

func (p *promProxy) Setup() error {
	var err error

	if err = util.WaitForDeploymentsReady(tc.Kube.Namespace, time.Minute*2, tc.Kube.KubeConfig); err != nil {
		return fmt.Errorf("could not establish prometheus proxy: pods not ready: %v", err)
	}

	if err = p.portForward("app=prometheus", prometheusPort, prometheusPort); err != nil {
		return err
	}

	if err = p.portForward("istio-mixer-type=telemetry", mixerMetricsPort, mixerMetricsPort); err != nil {
		return err
	}

	return p.portForward("app=productpage", productPagePort, "9080")
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
func TestMain(m *testing.M) {
	flag.Parse()
	check(framework.InitLogging(), "cannot setup logging")
	check(setTestConfig(), "could not create TestConfig")
	tc.Cleanup.RegisterCleanable(tc)
	os.Exit(tc.RunTest(m))
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
			AppYaml:    getBookinfoResourcePath(bookinfoYaml),
			KubeInject: true,
		},
		{
			AppYaml:    getBookinfoResourcePath(bookinfoRatingsv2Yaml),
			KubeInject: true,
		},
		{
			AppYaml:    getBookinfoResourcePath(bookinfoDbYaml),
			KubeInject: true,
		},
		{
			AppYaml:    util.GetResourcePath(sleepYaml + "." + yamlExtension),
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

func fatalf(t *testing.T, format string, args ...interface{}) {
	dumpK8Env()
	t.Fatalf(format, args...)
}

func errorf(t *testing.T, format string, args ...interface{}) {
	dumpK8Env()
	t.Errorf(format, args...)
}

func TestMetric(t *testing.T) {
	checkMetricReport(t, destLabel, fqdn("productpage"))
}

func TestIngressMetric(t *testing.T) {
	checkMetricReport(t, srcWorkloadLabel, "istio-"+ingressName)
}

// checkMetricReport checks whether report works for the given service
// by visiting productpage and comparing request_count metric.
func checkMetricReport(t *testing.T, label, labelValue string) {
	// setup prometheus API
	promAPI, err := promAPI()
	if err != nil {
		t.Fatalf("Could not build prometheus API client: %v", err)
	}

	t.Logf("Check request count metric for %q=%q", label, labelValue)

	// establish baseline by querying request count metric.
	t.Log("establishing metrics baseline for test...")
	query := fmt.Sprintf("istio_requests_total{%s=\"%s\"}", label, labelValue)
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

	// visit product page.
	if errNew := visitProductPage(productPageTimeout, http.StatusOK); errNew != nil {
		t.Fatalf("Test app setup failure: %v", errNew)
	}
	allowPrometheusSync()

	t.Log("Successfully sent request(s) to /productpage; checking metrics...")

	query = fmt.Sprintf("istio_requests_total{%s=\"%s\",%s=\"200\"}", label, labelValue, responseCodeLabel)
	t.Logf("prometheus query: %s", query)
	value, err = promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		fatalf(t, "Could not get metrics from prometheus: %v", err)
	}
	t.Logf("promvalue := %s", value.String())

	got, err := vectorValue(value, map[string]string{})
	if err != nil {
		t.Logf("prometheus values for istio_requests_total:\n%s", promDump(promAPI, "istio_requests_total"))
		fatalf(t, "Could not find metric value: %v", err)
	}
	t.Logf("Got request_count (200s) of: %f", got)
	t.Logf("Actual new requests observed: %f", got-prior200s)

	want := float64(1)
	if (got - prior200s) < want {
		t.Logf("prometheus values for istio_requests_total:\n%s", promDump(promAPI, "istio_requests_total"))
		errorf(t, "Bad metric value: got %f, want at least %f", got-prior200s, want)
	}
}

func TestTcpMetrics(t *testing.T) {
	if err := replaceRouteRule(tcpDbRule); err != nil {
		t.Fatalf("Could not update reviews routing rule: %v", err)
	}
	defer func() {
		if err := deleteRoutingConfig(tcpDbRule); err != nil {
			t.Fatalf("Could not delete reviews routing rule: %v", err)
		}
	}()
	allowRuleSync()

	if err := visitProductPage(productPageTimeout, http.StatusOK); err != nil {
		t.Fatalf("Test app setup failure: %v", err)
	}
	allowPrometheusSync()

	log.Info("Successfully sent request(s) to /productpage; checking metrics...")

	promAPI, err := promAPI()
	if err != nil {
		fatalf(t, "Could not build prometheus API client: %v", err)
	}
	query := fmt.Sprintf("istio_tcp_sent_bytes_total{destination_app=\"%s\"}", "mongodb")
	t.Logf("prometheus query: %s", query)
	value, err := promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		fatalf(t, "Could not get metrics from prometheus: %v", err)
	}
	log.Infof("promvalue := %s", value.String())

	got, err := vectorValue(value, map[string]string{})
	if err != nil {
		t.Logf("prometheus values for istio_tcp_sent_bytes_total:\n%s", promDump(promAPI, "istio_tcp_sent_bytes_total"))
		fatalf(t, "Could not find metric value: %v", err)
	}
	t.Logf("tcp_sent_bytes_total: %f", got)
	want := float64(1)
	if got < want {
		t.Logf("prometheus values for istio_tcp_sent_bytes_total:\n%s", promDump(promAPI, "istio_tcp_sent_bytes_total"))
		errorf(t, "Bad metric value: got %f, want at least %f", got, want)
	}

	query = fmt.Sprintf("istio_tcp_received_bytes_total{destination_app=\"%s\"}", "mongodb")
	t.Logf("prometheus query: %s", query)
	value, err = promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		fatalf(t, "Could not get metrics from prometheus: %v", err)
	}
	log.Infof("promvalue := %s", value.String())

	got, err = vectorValue(value, map[string]string{})
	if err != nil {
		t.Logf("prometheus values for istio_tcp_received_bytes_total:\n%s", promDump(promAPI, "istio_tcp_received_bytes_total"))
		fatalf(t, "Could not find metric value: %v", err)
	}
	t.Logf("tcp_received_bytes_total: %f", got)
	if got < want {
		t.Logf("prometheus values for istio_tcp_received_bytes_total:\n%s", promDump(promAPI, "istio_tcp_received_bytes_total"))
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

	log.Info("Successfully sent request(s) to /productpage; checking metrics...")
	allowPrometheusSync()
	promAPI, err := promAPI()
	if err != nil {
		fatalf(t, "Could not build prometheus API client: %v", err)
	}
	query := fmt.Sprintf("istio_response_bytes_count{%s=\"%s\",%s=\"200\"}", destLabel, fqdn("productpage"), responseCodeLabel)
	t.Logf("prometheus query: %s", query)
	value, err := promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		fatalf(t, "Could not get metrics from prometheus: %v", err)
	}
	log.Infof("promvalue := %s", value.String())

	got, err := vectorValue(value, map[string]string{})
	if err != nil {
		t.Logf("prometheus values for istio_response_bytes_count:\n%s", promDump(promAPI, "istio_response_bytes_count"))
		t.Logf("prometheus values for istio_requests_total:\n%s", promDump(promAPI, "istio_requests_total"))
		fatalf(t, "Could not find metric value: %v", err)
	}
	want := float64(1)
	if got < want {
		t.Logf("prometheus values for istio_response_bytes_count:\n%s", promDump(promAPI, "istio_response_bytes_count"))
		t.Logf("prometheus values for istio_requests_total:\n%s", promDump(promAPI, "istio_requests_total"))
		errorf(t, "Bad metric value: got %f, want at least %f", got, want)
	}
}

func TestKubeenvMetrics(t *testing.T) {
	if err := applyMixerRule(kubeenvTelemetryRule); err != nil {
		fatalf(t, "could not create required mixer rule: %v", err)
	}

	defer func() {
		if err := deleteMixerRule(kubeenvTelemetryRule); err != nil {
			t.Logf("could not clear rule: %v", err)
		}
	}()

	allowRuleSync()

	if err := visitProductPage(productPageTimeout, http.StatusOK); err != nil {
		fatalf(t, "Test app setup failure: %v", err)
	}

	log.Info("Successfully sent request(s) to /productpage; checking metrics...")
	allowPrometheusSync()
	promAPI, err := promAPI()
	if err != nil {
		fatalf(t, "Could not build prometheus API client: %v", err)
	}
	productPagePod, err := podID("app=productpage")
	if err != nil {
		fatalf(t, "Could not get productpage pod ID: %v", err)
	}
	productPageWorkloadName, productPageOwner, productPageUID, err := deployment("app=productpage")
	if err != nil {
		fatalf(t, "Could not get productpage deployment metadata: %v", err)
	}
	ingressPod, err := podID(fmt.Sprintf("istio=%s", ingressName))
	if err != nil {
		fatalf(t, "Could not get ingress pod ID: %v", err)
	}
	ingressWorkloadName, ingressOwner, ingressUID, err := deployment(fmt.Sprintf("istio=%s", ingressName))
	if err != nil {
		fatalf(t, "Could not get ingress deployment metadata: %v", err)
	}

	query := fmt.Sprintf("istio_kube_request_count{%s=\"%s\",%s=\"%s\",%s=\"%s\",%s=\"%s\",%s=\"%s\",%s=\"%s\",%s=\"%s\",%s=\"%s\",%s=\"%s\",%s=\"200\"}",
		srcPodLabel, ingressPod, srcWorkloadLabel, ingressWorkloadName, srcOwnerLabel, ingressOwner, srcUIDLabel, ingressUID,
		destPodLabel, productPagePod, destWorkloadLabel, productPageWorkloadName, destOwnerLabel, productPageOwner, destUIDLabel, productPageUID,
		destContainerLabel, "productpage", responseCodeLabel)
	t.Logf("prometheus query: %s", query)
	value, err := promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		fatalf(t, "Could not get metrics from prometheus: %v", err)
	}
	log.Infof("promvalue := %s", value.String())

	got, err := vectorValue(value, map[string]string{})
	if err != nil {
		t.Logf("prometheus values for istio_kube_request_count:\n%s", promDump(promAPI, "istio_kube_request_count"))
		fatalf(t, "Error get metric value: %v", err)
	}
	want := float64(1)
	if got < want {
		errorf(t, "Bad metric value: got %f, want at least %f", got, want)
	}
}

func TestDenials(t *testing.T) {
	testDenials(t, denialRule)
}

func TestIngressDenials(t *testing.T) {
	testDenials(t, ingressDenialRule)
}

// testDenials checks that the given rule could deny requests to productpage unless x-user is set in header.
func testDenials(t *testing.T, rule string) {
	if err := visitProductPage(productPageTimeout, http.StatusOK); err != nil {
		fatalf(t, "Test app setup failure: %v", err)
	}

	// deny rule will deny all requests to product page unless
	// ["x-user"] header is set.
	log.Infof("Denials: block productpage if x-user header is john")
	if err := applyMixerRule(rule); err != nil {
		fatalf(t, "could not create required mixer rule: %v", err)
	}

	defer func() {
		if err := deleteMixerRule(rule); err != nil {
			t.Logf("could not clear rule: %v", err)
		}
	}()

	time.Sleep(10 * time.Second)

	// Product page should not be accessible anymore.
	log.Infof("Denials: ensure productpage is denied access for user john")
	if err := visitProductPage(productPageTimeout, http.StatusForbidden, &header{"x-user", "john"}); err != nil {
		fatalf(t, "product page was not denied: %v", err)
	}

	// Product page *should be* accessible with x-user header.
	log.Infof("Denials: ensure productpage is accessible for testuser")
	if err := visitProductPage(productPageTimeout, http.StatusOK, &header{"x-user", "testuser"}); err != nil {
		fatalf(t, "product page was not denied: %v", err)
	}
}

// TestIngressCheckCache tests that check cache works in Ingress.
func TestIngressCheckCache(t *testing.T) {
	//t.Skip("https://github.com/istio/istio/issues/6309")

	// Apply denial rule to istio-ingress, so that only request with ["x-user"] could go through.
	// This is to make the test focus on ingress check cache.
	t.Logf("block request through ingress if x-user header is john")
	if err := applyMixerRule(ingressDenialRule); err != nil {
		fatalf(t, "could not create required mixer rule: %v", err)
	}
	defer func() {
		if err := deleteMixerRule(ingressDenialRule); err != nil {
			t.Logf("could not clear rule: %v", err)
		}
	}()
	allowRuleSync()

	// Visit product page through ingress should all be denied.
	visit := func() error {
		url := fmt.Sprintf("%s/productpage", getIngressOrFail(t))
		// Send 100 requests in a relative short time to make sure check cache will be used.
		httpOptions := fhttp.HTTPOptions{
			URL: url,
		}
		httpOptions.AddAndValidateExtraHeader("x-user: john")
		opts := fhttp.HTTPRunnerOptions{
			RunnerOptions: periodic.RunnerOptions{
				QPS:        10,
				Exactly:    100,       // will make exactly 100 calls, so run for about 10 seconds
				NumThreads: 5,         // get the same number of calls per connection (100/5=20)
				Out:        os.Stderr, // only needed because of log capture issue
			},
			HTTPOptions: httpOptions,
		}

		_, err := fhttp.RunHTTPTest(&opts)
		if err != nil {
			return fmt.Errorf("generating traffic via fortio failed: %v", err)
		}
		return nil
	}
	testCheckCache(t, visit)
}

func getIngressOrFail(t *testing.T) string {
	return tc.Kube.IngressGatewayOrFail(t)
}

// TestCheckCache tests that check cache works within the mesh.
func TestCheckCache(t *testing.T) {
	// Get pod id of sleep app.
	pod, err := podID("app=sleep")
	if err != nil {
		fatalf(t, "fail getting pod id of sleep %v", err)
	}
	url := fmt.Sprintf("http://productpage.%s:9080/health", tc.Kube.Namespace)

	// visit calls product page health handler with sleep app.
	visit := func() error {
		return visitWithApp(url, pod, "sleep", 100)
	}
	testCheckCache(t, visit)
}

// testCheckCache verifies check cache is used when calling the given visit function
// by comparing the check call metric.
func testCheckCache(t *testing.T, visit func() error) {
	promAPI, err := promAPI()
	if err != nil {
		fatalf(t, "Could not build prometheus API client: %v", err)
	}

	// Get check cache hit baseline.
	t.Log("Query prometheus to get baseline cache hits...")
	prior, err := getCheckCacheHits(promAPI)
	if err != nil {
		fatalf(t, "Unable to retrieve valid cached hit number: %v", err)
	}

	t.Logf("Baseline cache hits: %v", prior)
	t.Log("Start to call visit function...")
	if err = visit(); err != nil {
		fatalf(t, "%v", err)
	}

	allowPrometheusSync()
	t.Log("Query promethus to get new cache hits number...")
	// Get new check cache hit.
	got, err := getCheckCacheHits(promAPI)
	if err != nil {
		fatalf(t, "Unable to retrieve valid cached hit number: %v", err)
	}
	t.Logf("New cache hits: %v", got)

	// At least 1 call should be cache hit.
	want := float64(1)
	if (got - prior) < want {
		errorf(t, "Check cache hit: %v is less than expected: %v", got-prior, want)
	}
}

func fetchRequestCount(t *testing.T, promAPI v1.API, service string) (prior429s float64, prior200s float64, value model.Value) {
	var err error
	t.Log("Establishing metrics baseline for test...")
	query := fmt.Sprintf("istio_requests_total{%s=\"%s\"}", destLabel, fqdn(service))
	t.Logf("prometheus query: %s", query)
	value, err = promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		fatalf(t, "Could not get metrics from prometheus: %v", err)
	}

	prior429s, err = vectorValue(value, map[string]string{responseCodeLabel: "429", reporterLabel: "destination"})
	if err != nil {
		t.Logf("error getting prior 429s, using 0 as value (msg: %v)", err)
		prior429s = 0
	}

	prior200s, err = vectorValue(value, map[string]string{responseCodeLabel: "200", reporterLabel: "destination"})
	if err != nil {
		t.Logf("error getting prior 200s, using 0 as value (msg: %v)", err)
		prior200s = 0
	}
	t.Logf("Baseline established: prior200s = %f, prior429s = %f", prior200s, prior429s)

	return prior429s, prior200s, value
}

func sendTraffic(t *testing.T, msg string, calls int64) *fhttp.HTTPRunnerResults {
	t.Log(msg)
	url := fmt.Sprintf("%s/productpage", getIngressOrGatewayOrFail(t))

	// run at a high enough QPS (here 10) to ensure that enough
	// traffic is generated to trigger 429s from the 1 QPS rate limit rule
	opts := fhttp.HTTPRunnerOptions{
		RunnerOptions: periodic.RunnerOptions{
			QPS:        10,
			Exactly:    calls,     // will make exactly 300 calls, so run for about 30 seconds
			NumThreads: 5,         // get the same number of calls per connection (300/5=60)
			Out:        os.Stderr, // Only needed because of log capture issue
		},
		HTTPOptions: fhttp.HTTPOptions{
			URL: url,
		},
	}
	// productpage should still return 200s when ratings is rate-limited.
	res, err := fhttp.RunHTTPTest(&opts)
	if err != nil {
		fatalf(t, "Generating traffic via fortio failed: %v", err)
	}
	return res
}

func TestMetricsAndRateLimitAndRulesAndBookinfo(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/6309")

	if err := replaceRouteRule(routeReviewsV3Rule); err != nil {
		fatalf(t, "Could not create replace reviews routing rule: %v", err)
	}
	defer func() {
		if err := deleteRoutingConfig(routeReviewsV3Rule); err != nil {
			t.Fatalf("Could not delete reviews routing rule: %v", err)
		}
	}()

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

	initPrior429s, _, _ := fetchRequestCount(t, promAPI, "ratings")

	_ = sendTraffic(t, "Warming traffic...", 150)
	allowPrometheusSync()
	prior429s, prior200s, _ := fetchRequestCount(t, promAPI, "ratings")
	// check if at least one more prior429 was reported
	if prior429s-initPrior429s < 1 {
		fatalf(t, "no 429 is allotted time: prior429s:%v", prior429s)
	}

	res := sendTraffic(t, "Sending traffic...", 300)
	allowPrometheusSync()

	totalReqs := res.DurationHistogram.Count
	succReqs := float64(res.RetCodes[http.StatusOK])
	badReqs := res.RetCodes[http.StatusBadRequest]
	actualDuration := res.ActualDuration.Seconds() // can be a bit more than requested

	log.Info("Successfully sent request(s) to /productpage; checking metrics...")
	t.Logf("Fortio Summary: %d reqs (%f rps, %f 200s (%f rps), %d 400s - %+v)",
		totalReqs, res.ActualQPS, succReqs, succReqs/actualDuration, badReqs, res.RetCodes)

	// consider only successful requests (as recorded at productpage service)
	callsToRatings := succReqs

	// the rate-limit is 1 rps
	want200s := 1. * actualDuration

	// everything in excess of 200s should be 429s (ideally)
	want429s := callsToRatings - want200s

	t.Logf("Expected Totals: 200s: %f (%f rps), 429s: %f (%f rps)", want200s, want200s/actualDuration, want429s, want429s/actualDuration)

	// if we received less traffic than the expected enforced limit to ratings
	// then there is no way to determine if the rate limit was applied at all
	// and for how much traffic. log all metrics and abort test.
	if callsToRatings < want200s {
		t.Logf("full set of prometheus metrics:\n%s", promDump(promAPI, "istio_requests_total"))
		fatalf(t, "Not enough traffic generated to exercise rate limit: ratings_reqs=%f, want200s=%f", callsToRatings, want200s)
	}

	_, _, value := fetchRequestCount(t, promAPI, "ratings")
	log.Infof("promvalue := %s", value.String())

	got, err := vectorValue(value, map[string]string{responseCodeLabel: "429", "destination_version": "v1"})
	if err != nil {
		t.Logf("prometheus values for istio_requests_total:\n%s", promDump(promAPI, "istio_requests_total"))
		errorf(t, "Could not find 429s: %v", err)
		got = 0 // want to see 200 rate even if no 429s were recorded
	}

	// Lenient calculation TODO: tighten/simplify
	want := math.Floor(want429s * .25)

	got = got - prior429s

	t.Logf("Actual 429s: %f (%f rps)", got, got/actualDuration)

	// check resource exhausted
	if got < want {
		t.Logf("prometheus values for istio_requests_total:\n%s", promDump(promAPI, "istio_requests_total"))
		errorf(t, "Bad metric value for rate-limited requests (429s): got %f, want at least %f", got, want)
	}

	got, err = vectorValue(value, map[string]string{responseCodeLabel: "200", "destination_version": "v1"})
	if err != nil {
		t.Logf("prometheus values for istio_requests_total:\n%s", promDump(promAPI, "istio_requests_total"))
		errorf(t, "Could not find successes value: %v", err)
		got = 0
	}

	got = got - prior200s

	t.Logf("Actual 200s: %f (%f rps), expecting ~1 rps", got, got/actualDuration)

	// establish some baseline to protect against flakiness due to randomness in routing
	// and to allow for leniency in actual ceiling of enforcement (if 10 is the limit, but we allow slightly
	// less than 10, don't fail this test).
	want = math.Floor(want200s * .25)

	// check successes
	if got < want {
		t.Logf("prometheus values for istio_requests_total:\n%s", promDump(promAPI, "istio_requests_total"))
		errorf(t, "Bad metric value for successful requests (200s): got %f, want at least %f", got, want)
	}
	// TODO: until https://github.com/istio/istio/issues/3028 is fixed, use 25% - should be only 5% or so
	want200s = math.Ceil(want200s * 1.5)
	if got > want200s {
		t.Logf("prometheus values for istio_requests_total:\n%s", promDump(promAPI, "istio_requests_total"))
		errorf(t, "Bad metric value for successful requests (200s): got %f, want at most %f", got, want200s)
	}
}

func testRedisQuota(t *testing.T, quotaRule string) {
	if err := replaceRouteRule(routeReviewsV3Rule); err != nil {
		fatalf(t, "Could not create replace reviews routing rule: %v", err)
	}
	defer func() {
		if err := deleteRoutingConfig(routeReviewsV3Rule); err != nil {
			t.Fatalf("Could not delete reviews routing rule: %v", err)
		}
	}()

	if err := util.KubeScale(tc.Kube.Namespace, "deployment/istio-policy", 2, tc.Kube.KubeConfig); err != nil {
		fatalf(t, "Could not scale up istio-policy pod: %v", err)
	}
	defer func() {
		if err := util.KubeScale(tc.Kube.Namespace, "deployment/istio-policy", 1, tc.Kube.KubeConfig); err != nil {
			t.Fatalf("Could not scale down istio-policy pod.: %v", err)
		}
		allowRuleSync()
	}()

	// Deploy Tiller if not already running.
	if err := util.CheckPodRunning("kube-system", "name=tiller", tc.Kube.KubeConfig); err != nil {
		if errDeployTiller := tc.Kube.DeployTiller(); errDeployTiller != nil {
			fatalf(t, "Failed to deploy helm tiller: %v", errDeployTiller)
		}
	}

	setValue := "--set usePassword=false,persistence.enabled=false"
	if err := util.HelmInstall(redisInstallDir, redisInstallName, tc.Kube.Namespace, setValue); err != nil {
		fatalf(t, "Helm install %s failed, setValue=%s", redisInstallDir, setValue)
	}
	defer func() {
		if err := util.HelmDelete(redisInstallName); err != nil {
			t.Logf("Could not delete %s: %v", redisInstallName, err)
		}
	}()

	allowRuleSync()

	// the rate limit rule applies a max rate limit of 1 rps to the ratings service.
	if err := applyMixerRule(quotaRule); err != nil {
		fatalf(t, "could not create required mixer rule: %v", err)
	}
	defer func() {
		if err := deleteMixerRule(quotaRule); err != nil {
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
	_ = sendTraffic(t, "Warming traffic...", 150)
	allowPrometheusSync()
	initPrior429s, _, _ := fetchRequestCount(t, promAPI, "ratings")

	_ = sendTraffic(t, "Warming traffic...", 150)
	allowPrometheusSync()
	prior429s, prior200s, _ := fetchRequestCount(t, promAPI, "ratings")
	// check if at least one more prior429 was reported
	if prior429s-initPrior429s < 1 {
		fatalf(t, "no 429 is allotted time: prior429s:%v", prior429s)
	}

	res := sendTraffic(t, "Sending traffic...", 300)
	allowPrometheusSync()

	totalReqs := res.DurationHistogram.Count
	succReqs := float64(res.RetCodes[http.StatusOK])
	badReqs := res.RetCodes[http.StatusBadRequest]
	actualDuration := res.ActualDuration.Seconds() // can be a bit more than requested

	log.Info("Successfully sent request(s) to /productpage; checking metrics...")
	t.Logf("Fortio Summary: %d reqs (%f rps, %f 200s (%f rps), %d 400s - %+v)",
		totalReqs, res.ActualQPS, succReqs, succReqs/actualDuration, badReqs, res.RetCodes)

	// consider only successful requests (as recorded at productpage service)
	callsToRatings := succReqs

	// the rate-limit is 0.1 rps from ratings to reviews.
	want200s := 0.1 * actualDuration

	// everything in excess of 200s should be 429s (ideally)
	want429s := callsToRatings - want200s

	t.Logf("Expected Totals: 200s: %f (%f rps), 429s: %f (%f rps)", want200s, want200s/actualDuration, want429s, want429s/actualDuration)

	// if we received less traffic than the expected enforced limit to ratings
	// then there is no way to determine if the rate limit was applied at all
	// and for how much traffic. log all metrics and abort test.
	if callsToRatings < want200s {
		attributes := []string{fmt.Sprintf("%s=\"%s\"", destLabel, fqdn("ratings"))}
		t.Logf("full set of prometheus metrics for ratings:\n%s", promDumpWithAttributes(promAPI, "istio_requests_total", attributes))
		fatalf(t, "Not enough traffic generated to exercise rate limit: ratings_reqs=%f, want200s=%f", callsToRatings, want200s)
	}

	_, _, value := fetchRequestCount(t, promAPI, "ratings")
	log.Infof("promvalue := %s", value.String())

	got, err := vectorValue(value, map[string]string{responseCodeLabel: "429", reporterLabel: "destination"})
	if err != nil {
		attributes := []string{fmt.Sprintf("%s=\"%s\"", destLabel, fqdn("ratings")),
			fmt.Sprintf("%s=\"%d\"", responseCodeLabel, 429), fmt.Sprintf("%s=\"%s\"", reporterLabel, "destination")}
		t.Logf("prometheus values for istio_requests_total for 429's:\n%s", promDumpWithAttributes(promAPI, "istio_requests_total", attributes))
		errorf(t, "Could not find 429s: %v", err)
		got = 0 // want to see 200 rate even if no 429s were recorded
	}

	want := math.Floor(want429s * 0.70)

	got = got - prior429s

	t.Logf("Actual 429s: %f (%f rps)", got, got/actualDuration)

	// check resource exhausted
	if got < want {
		attributes := []string{fmt.Sprintf("%s=\"%s\"", destLabel, fqdn("ratings")),
			fmt.Sprintf("%s=\"%d\"", responseCodeLabel, 429), fmt.Sprintf("%s=\"%s\"", reporterLabel, "destination")}
		t.Logf("prometheus values for istio_requests_total for 429's:\n%s", promDumpWithAttributes(promAPI, "istio_requests_total", attributes))
		errorf(t, "Bad metric value for rate-limited requests (429s): got %f, want at least %f", got, want)
	}

	got, err = vectorValue(value, map[string]string{responseCodeLabel: "200", reporterLabel: "destination"})
	if err != nil {
		attributes := []string{fmt.Sprintf("%s=\"%s\"", destLabel, fqdn("ratings")),
			fmt.Sprintf("%s=\"%d\"", responseCodeLabel, 200), fmt.Sprintf("%s=\"%s\"", reporterLabel, "destination")}
		t.Logf("prometheus values for istio_requests_total for 200's:\n%s", promDumpWithAttributes(promAPI, "istio_requests_total", attributes))
		errorf(t, "Could not find successes value: %v", err)
		got = 0
	}

	got = got - prior200s

	t.Logf("Actual 200s: %f (%f rps), expecting ~1 rps", got, got/actualDuration)

	// establish some baseline to protect against flakiness due to randomness in routing
	// and to allow for leniency in actual ceiling of enforcement (if 10 is the limit, but we allow slightly
	// less than 10, don't fail this test).
	want = math.Floor(want200s * 0.70)

	// check successes
	if got < want {
		attributes := []string{fmt.Sprintf("%s=\"%s\"", destLabel, fqdn("ratings")),
			fmt.Sprintf("%s=\"%d\"", responseCodeLabel, 200), fmt.Sprintf("%s=\"%s\"", reporterLabel, "destination")}
		t.Logf("prometheus values for istio_requests_total for 200's:\n%s", promDumpWithAttributes(promAPI, "istio_requests_total", attributes))
		errorf(t, "Bad metric value for successful requests (200s): got %f, want at least %f", got, want)
	}
	// TODO: until https://github.com/istio/istio/issues/3028 is fixed, use 25% - should be only 5% or so
	want200s = math.Ceil(want200s * 1.5)
	if got > want200s {
		attributes := []string{fmt.Sprintf("%s=\"%s\"", destLabel, fqdn("ratings")),
			fmt.Sprintf("%s=\"%d\"", responseCodeLabel, 200), fmt.Sprintf("%s=\"%s\"", reporterLabel, "destination")}
		t.Logf("prometheus values for istio_requests_total for 200's:\n%s", promDumpWithAttributes(promAPI, "istio_requests_total", attributes))
		errorf(t, "Bad metric value for successful requests (200s): got %f, want at most %f", got, want200s)
	}
}

func TestRedisQuotaRollingWindow(t *testing.T) {
	testRedisQuota(t, redisQuotaRollingWindowRule)
}

func TestRedisQuotaFixedWindow(t *testing.T) {
	testRedisQuota(t, redisQuotaFixedWindowRule)
}

func TestMixerReportingToMixer(t *testing.T) {
	// setup prometheus API
	promAPI, err := promAPI()
	if err != nil {
		t.Fatalf("Could not build prometheus API client: %v", err)
	}

	// ensure that some traffic has gone through mesh successfully
	if err = visitProductPage(productPageTimeout, http.StatusOK); err != nil {
		fatalf(t, "Test app setup failure: %v", err)
	}

	log.Info("Successfully sent request(s) to productpage app through ingress.")
	allowPrometheusSync()

	t.Logf("Validating metrics with 'istio-policy' have been generated... ")
	query := fmt.Sprintf("sum(istio_requests_total{%s=\"%s\"}) by (%s)", destLabel, fqdn("istio-policy"), srcLabel)
	t.Logf("Prometheus query: %s", query)
	value, err := promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		t.Fatalf("Could not get metrics from prometheus: %v", err)
	}

	if value.Type() != model.ValVector {
		t.Fatalf("Expected ValVector from prometheus, got %T", value)
	}

	if vec := value.(model.Vector); len(vec) < 1 {
		t.Logf("Values for istio_requests_total:\n%s", promDump(promAPI, "istio_requests_total"))
		t.Errorf("Expected at least one metric with 'istio-policy' as the destination, got %d", len(vec))
	}

	t.Logf("Validating metrics with 'istio-telemetry' have been generated... ")
	query = fmt.Sprintf("sum(istio_requests_total{%s=\"%s\"}) by (%s)", destLabel, fqdn("istio-telemetry"), srcLabel)
	t.Logf("Prometheus query: %s", query)
	value, err = promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		t.Fatalf("Could not get metrics from prometheus: %v", err)
	}

	if value.Type() != model.ValVector {
		t.Fatalf("Expected ValVector from prometheus, got %T", value)
	}

	if vec := value.(model.Vector); len(vec) < 1 {
		t.Logf("Values for istio_requests_total:\n%s", promDump(promAPI, "istio_requests_total"))
		t.Errorf("Expected at least one metric with 'istio-telemetry' as the destination, got %d", len(vec))
	}

	t.Logf("Validating Mixer access logs show Check() and Report() calls...")
	logs, err :=
		util.Shell(`kubectl -n %s logs -l istio-mixer-type=telemetry -c mixer --tail 1000 | grep -e "%s" -e "%s"`,
			tc.Kube.Namespace, checkPath, reportPath)
	if err != nil {
		t.Fatalf("Error retrieving istio-telemetry logs: %v", err)
	}
	wantLines := 4
	gotLines := strings.Count(logs, "\n")
	if gotLines < wantLines {
		t.Errorf("Expected at least %v lines of Mixer-specific access logs, got %d", wantLines, gotLines)
	}

}

func allowRuleSync() {
	log.Info("Sleeping to allow rules to take effect...")
	time.Sleep(1 * time.Minute)
}

func allowPrometheusSync() {
	log.Info("Sleeping to allow prometheus to record metrics...")
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

// promDumpWithAttributes is used to get all of the recorded values of a metric for particular attributes.
// Attributes have to be of format %s=\"%s\"
func promDumpWithAttributes(promAPI v1.API, metric string, attributes []string) string {
	var err error
	query := fmt.Sprintf("%s{%s}", metric, strings.Join(attributes, ", "))
	value, err := promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		return ""
	}

	return value.String()
}

func vectorValue(val model.Value, labels map[string]string) (float64, error) {
	if val.Type() != model.ValVector {
		return 0, fmt.Errorf("value not a model.Vector; was %s", val.Type().String())
	}

	value := val.(model.Vector)
	valueCount := 0.0
	for _, sample := range value {
		metric := sample.Metric
		nameCount := len(labels)
		for k, v := range metric {
			if labelVal, ok := labels[string(k)]; ok && labelVal == string(v) {
				nameCount--
			}
		}
		if nameCount == 0 {
			valueCount += float64(sample.Value)
		}
	}
	if valueCount > 0.0 {
		return valueCount, nil
	}
	return 0, fmt.Errorf("value not found for %#v", labels)
}

// checkProductPageDirect
func checkProductPageDirect() {
	log.Info("checkProductPageDirect")
	dumpURL("http://localhost:"+productPagePort+"/productpage", false)
}

// dumpMixerMetrics fetch metrics directly from mixer and dump them
func dumpMixerMetrics() {
	log.Info("dumpMixerMetrics")
	dumpURL("http://localhost:"+mixerMetricsPort+"/metrics", true)
}

func dumpURL(url string, dumpContents bool) {
	clnt := &http.Client{
		Timeout: 1 * time.Minute,
	}
	status, contents, err := get(clnt, url)
	log.Infof("%s ==> %d, <%v>", url, status, err)
	if dumpContents {
		log.Infof("%v\n", contents)
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
		log.Warnf("Error communicating with %s: %v", url, err)
	} else {
		defer closeResponseBody(resp)
		log.Infof("Get from %s: %s (%d)", url, resp.Status, resp.StatusCode)
		var ba []byte
		ba, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Warnf("Unable to connect to read from %s: %v", url, err)
			return
		}
		contents = string(ba)
		status = resp.StatusCode
	}
	return
}

func getIngressOrGateway() (string, error) {
	return tc.Kube.IngressGateway()
}

func getIngressOrGatewayOrFail(t *testing.T) string {
	return tc.Kube.IngressGatewayOrFail(t)
}

func visitProductPage(timeout time.Duration, wantStatus int, headers ...*header) error {
	start := time.Now()
	clnt := &http.Client{
		Timeout: 1 * time.Minute,
	}

	gateway, err := getIngressOrGateway()
	if err != nil {
		return err
	}

	url := gateway + "/productpage"

	for {
		status, _, err := get(clnt, url, headers...)
		if err != nil {
			log.Warnf("Unable to connect to product page: %v", err)
		}

		if status == wantStatus {
			log.Infof("Got %d response from product page!", wantStatus)
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

// visitWithApp visits the given url by curl in the given container.
func visitWithApp(url string, pod string, container string, num int) error {
	cmd := fmt.Sprintf("kubectl exec %s -n %s -c %s -- bash -c 'for ((i=0; i<%d; i++)); do curl -m 0.1 -i -s %s; done'",
		pod, tc.Kube.Namespace, container, num, url)
	log.Infof("Visit %s for %d times with the following command: %v", url, num, cmd)
	_, err := util.ShellMuteOutput(cmd)
	if err != nil {
		return fmt.Errorf("error excuting command: %s error: %v", cmd, err)
	}
	return nil
}

// getCheckCacheHits returned the total number of check cache hits in this cluster.
func getCheckCacheHits(promAPI v1.API) (float64, error) {
	log.Info("Get number of cached check calls")
	query := fmt.Sprintf("envoy_http_mixer_filter_total_check_calls")
	log.Infof("prometheus query: %s", query)
	value, err := promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		log.Infof("Could not get remote check calls metric from prometheus: %v", err)
		return 0, nil
	}
	totalCheck, err := vectorValue(value, map[string]string{})
	if err != nil {
		log.Infof("error getting total check, using 0 as value (msg: %v)", err)
		totalCheck = 0
	}

	query = fmt.Sprintf("envoy_http_mixer_filter_total_remote_check_calls")
	log.Infof("prometheus query: %s", query)
	value, err = promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		log.Infof("Could not get remote check calls metric from prometheus: %v", err)
		return 0, nil
	}
	remoteCheck, err := vectorValue(value, map[string]string{})
	if err != nil {
		log.Infof("error getting total check, using 0 as value (msg: %v)", err)
		remoteCheck = 0
	}

	if remoteCheck > totalCheck {
		// Remote check calls should always be less than or equal to total check calls.
		return 0, fmt.Errorf("check call metric is invalid: remote check call %v is more than total check call %v", remoteCheck, totalCheck)
	}
	log.Infof("Total check call is %v and remote check call is %v", totalCheck, remoteCheck)
	// number of cached check call is the gap between total check calls and remote check calls.
	return totalCheck - remoteCheck, nil
}

func fqdn(service string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", service, tc.Kube.Namespace)
}

func replaceRouteRule(ruleName string) error {
	rule := filepath.Join(tc.rulesDir, ruleName+"."+yamlExtension)
	return util.KubeApply(tc.Kube.Namespace, rule, tc.Kube.KubeConfig)
}

func deleteRoutingConfig(ruleName string) error {
	rule := filepath.Join(tc.rulesDir, ruleName+"."+yamlExtension)
	return util.KubeDelete(tc.Kube.Namespace, rule, tc.Kube.KubeConfig)
}

func deleteMixerRule(ruleName string) error {
	return doMixerRule(ruleName, util.KubeDeleteContents)
}

func applyMixerRule(ruleName string) error {
	return doMixerRule(ruleName, util.KubeApplyContents)
}

type kubeDo func(namespace string, contents string, kubeconfig string) error

// doMixerRule
// New mixer rules contain fully qualified pointers to other
// resources, they must be replaced by the current namespace.
func doMixerRule(ruleName string, do kubeDo) error {
	rule := filepath.Join(tc.rulesDir, ruleName+"."+yamlExtension)
	cb, err := ioutil.ReadFile(rule)
	if err != nil {
		log.Errorf("Cannot read original yaml file %s", rule)
		return err
	}
	contents := string(cb)
	if !strings.Contains(contents, templateNamespace) {
		return fmt.Errorf("%s must contain %s so the it can replaced", rule, templateNamespace)
	}
	contents = strings.Replace(contents, templateNamespace, tc.Kube.Namespace, -1)
	return do(tc.Kube.Namespace, contents, tc.Kube.KubeConfig)
}

func getBookinfoResourcePath(resource string) string {
	return util.GetResourcePath(filepath.Join(bookinfoSampleDir, deploymentDir,
		resource+"."+yamlExtension))
}

func check(err error, msg string) {
	if err != nil {
		log.Errorf("%s. Error %s", msg, err)
		os.Exit(-1)
	}
}

func closeResponseBody(r *http.Response) {
	if err := r.Body.Close(); err != nil {
		log.Errora(err)
	}
}
