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

package pilot

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/multierr"

	"istio.io/istio/pilot/pkg/kube/inject"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	defaultRetryBudget      = 50
	retryDelay              = time.Second
	httpOK                  = "200"
	ingressAppName          = "ingress"
	ingressContainerName    = "ingress"
	defaultPropagationDelay = 10 * time.Second
)

var (
	tc = &testConfig{
		V1alpha1: true, //implies envoyv1
		V1alpha3: true, //implies envoyv2
		Ingress:  true,
		Egress:   true,
	}

	errAgain     = errors.New("try again")
	idRegex      = regexp.MustCompile("(?i)X-Request-Id=(.*)")
	versionRegex = regexp.MustCompile("ServiceVersion=(.*)")
	portRegex    = regexp.MustCompile("ServicePort=(.*)")
	codeRegex    = regexp.MustCompile("StatusCode=(.*)")
	hostRegex    = regexp.MustCompile("Host=(.*)")
)

func init() {
	flag.BoolVar(&tc.V1alpha1, "v1alpha1", tc.V1alpha1, "Enable / disable v1alpha1 routing rules.")
	flag.BoolVar(&tc.V1alpha3, "v1alpha3", tc.V1alpha3, "Enable / disable v1alpha3 routing rules.")
	flag.BoolVar(&tc.Ingress, "ingress", tc.Ingress, "Enable / disable Ingress tests.")
	flag.BoolVar(&tc.Egress, "egress", tc.Egress, "Enable / disable Egress tests.")
}

func TestMain(m *testing.M) {
	flag.Parse()
	check(framework.InitLogging(), "cannot setup logging")
	check(setTestConfig(), "could not create TestConfig")
	tc.Cleanup.RegisterCleanable(tc)
	os.Exit(tc.RunTest(m))
}

func setTestConfig() error {
	cc, err := framework.NewCommonConfig("pilot_test")
	if err != nil {
		return err
	}
	tc.CommonConfig = cc

	tc.Kube.InstallAddons = true // zipkin is used

	// Add mTLS auth exclusion policy.
	tc.Kube.MTLSExcludedServices = []string{fmt.Sprintf("fake-control.%s.svc.cluster.local", tc.Kube.Namespace)}

	appDir, err := ioutil.TempDir(os.TempDir(), "pilot_test")
	if err != nil {
		return err
	}
	tc.AppDir = appDir

	// Add additional apps for this test suite.
	apps := getApps(tc)
	for i := range apps {
		tc.Kube.AppManager.AddApp(&apps[i])
		if tc.Kube.RemoteKubeConfig != "" {
			tc.Kube.RemoteAppManager.AddApp(&apps[i])
		}
	}

	// Extra system configuration required for the pilot tests.
	tc.extraConfig = &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{
			"testdata/headless.yaml",
			"testdata/external-wikipedia.yaml",
			"testdata/externalbin.yaml",
		},
		kubeconfig: tc.Kube.KubeConfig,
	}
	return nil
}

func check(err error, msg string) {
	if err != nil {
		log.Errorf("%s. Error %s", msg, err)
		os.Exit(-1)
	}
}

// runRetriableTest runs the given test function the provided number of times.
func runRetriableTest(t *testing.T, testName string, retries int, f func() error, errorFunc ...func()) {
	t.Run(testName, func(t *testing.T) {
		// Run all request tests in parallel.
		// TODO(nmittler): Consider t.Parallel()?

		remaining := retries
		for {
			// Call the test function.
			remaining--
			err := f()
			if err == nil {
				// Test succeeded, we're done here.
				return
			}

			if remaining == 0 {
				// We're out of retries - fail the test now.
				for _, e := range errorFunc {
					if e != nil {
						e()
					}
				}
				t.Fatal(err)
			}

			// Wait for a bit before retrying.
			retries--
			time.Sleep(retryDelay)
		}
	})
}

// deployableConfig is a collection of configs that are applied/deleted as a single unit.
type deployableConfig struct {
	Namespace  string
	YamlFiles  []string
	applied    []string
	kubeconfig string
}

// Setup pushes the config and waits for it to propagate to all nodes in the cluster.
func (c *deployableConfig) Setup() error {
	c.applied = []string{}
	// Apply the configs.
	for _, yamlFile := range c.YamlFiles {
		if err := util.KubeApply(c.Namespace, yamlFile, c.kubeconfig); err != nil {
			// Run the teardown function now and return
			_ = c.Teardown()
			return err
		}
		c.applied = append(c.applied, yamlFile)
	}

	// Sleep for a while to allow the change to propagate.
	time.Sleep(c.propagationDelay())
	return nil
}

// Teardown deletes the deployed configuration.
func (c *deployableConfig) Teardown() error {
	err := c.TeardownNoDelay()

	// Sleep for a while to allow the change to propagate.
	time.Sleep(c.propagationDelay())
	return err
}

// Teardown deletes the deployed configuration.
func (c *deployableConfig) TeardownNoDelay() error {
	var err error
	for _, yamlFile := range c.applied {
		err = multierr.Append(err, util.KubeDelete(c.Namespace, yamlFile, c.kubeconfig))
	}
	c.applied = []string{}
	return err
}

func (c *deployableConfig) propagationDelay() time.Duration {
	return defaultPropagationDelay
}

type testConfig struct {
	*framework.CommonConfig
	AppDir      string
	V1alpha1    bool
	V1alpha3    bool
	Ingress     bool
	Egress      bool
	extraConfig *deployableConfig
}

// Setup initializes the test environment and waits for all pods to be in the running state.
func (t *testConfig) Setup() error {
	// Deploy additional configuration.
	err := t.extraConfig.Setup()

	// Wait for all the pods to be in the running state before starting tests.
	if err == nil && !util.CheckPodsRunning(t.Kube.Namespace, t.Kube.KubeConfig) {
		err = fmt.Errorf("can't get all pods running")
	}

	return err
}

// Teardown shuts down the test environment.
func (t *testConfig) Teardown() error {
	// Remove additional configuration.
	return t.extraConfig.Teardown()
}

func configVersions() []string {
	versions := []string{}
	if tc.V1alpha1 {
		versions = append(versions, "v1alpha1")
	}
	if tc.V1alpha3 {
		versions = append(versions, "v1alpha3")
	}
	return versions
}

func getApps(tc *testConfig) []framework.App {
	return []framework.App{
		// deploy a healthy mix of apps, with and without proxy
		getApp("t", "t", 8080, 80, 9090, 90, 7070, 70, "unversioned", false),
		getApp("a", "a", 8080, 80, 9090, 90, 7070, 70, "v1", true),
		getApp("b", "b", 80, 8080, 90, 9090, 70, 7070, "unversioned", true),
		getApp("c-v1", "c", 80, 8080, 90, 9090, 70, 7070, "v1", true),
		getApp("c-v2", "c", 80, 8080, 90, 9090, 70, 7070, "v2", true),
		getApp("d", "d", 80, 8080, 90, 9090, 70, 7070, "per-svc-auth", true),
		// Add another service without sidecar to test mTLS blacklisting (as in the e2e test
		// environment, pilot can see only services in the test namespaces). This service
		// will be listed in mtlsExcludedServices in the mesh config.
		getApp("e", "fake-control", 80, 8080, 90, 9090, 70, 7070, "fake-control", false),
	}
}

func getApp(deploymentName, serviceName string, port1, port2, port3, port4, port5, port6 int,
	version string, injectProxy bool) framework.App {
	// TODO(nmittler): Eureka does not support management ports ... should we support other registries?
	healthPort := "true"

	// Return the config.
	return framework.App{
		AppYamlTemplate: "testdata/app.yaml.tmpl",
		Template: map[string]string{
			"Hub":             tc.Kube.PilotHub(),
			"Tag":             tc.Kube.PilotTag(),
			"service":         serviceName,
			"deployment":      deploymentName,
			"port1":           strconv.Itoa(port1),
			"port2":           strconv.Itoa(port2),
			"port3":           strconv.Itoa(port3),
			"port4":           strconv.Itoa(port4),
			"port5":           strconv.Itoa(port5),
			"port6":           strconv.Itoa(port6),
			"version":         version,
			"istioNamespace":  tc.Kube.Namespace,
			"injectProxy":     strconv.FormatBool(injectProxy),
			"healthPort":      healthPort,
			"ImagePullPolicy": tc.Kube.ImagePullPolicy(),
		},
		KubeInject: injectProxy,
	}
}

// ClientRequest makes a request from inside the specified k8s container.
func ClientRequest(app, url string, count int, extra string) ClientResponse {
	out := ClientResponse{}

	pods := tc.Kube.GetAppPods()[app]
	if len(pods) == 0 {
		log.Errorf("Missing pod names for app %q", app)
		return out
	}

	pod := pods[0]
	cmd := fmt.Sprintf("client -url %s -count %d %s", url, count, extra)
	request, err := util.PodExec(tc.Kube.Namespace, pod, "app", cmd, true, tc.Kube.KubeConfig)
	if err != nil {
		log.Errorf("client request error %v for %s in %s", err, url, app)
		return out
	}

	out.Body = request

	ids := idRegex.FindAllStringSubmatch(request, -1)
	for _, id := range ids {
		out.ID = append(out.ID, id[1])
	}

	versions := versionRegex.FindAllStringSubmatch(request, -1)
	for _, version := range versions {
		out.Version = append(out.Version, version[1])
	}

	ports := portRegex.FindAllStringSubmatch(request, -1)
	for _, port := range ports {
		out.Port = append(out.Port, port[1])
	}

	codes := codeRegex.FindAllStringSubmatch(request, -1)
	for _, code := range codes {
		out.Code = append(out.Code, code[1])
	}

	hosts := hostRegex.FindAllStringSubmatch(request, -1)
	for _, host := range hosts {
		out.Host = append(out.Host, host[1])
	}

	return out
}

// ClientResponse represents a response to a client request.
type ClientResponse struct {
	// Body is the body of the response
	Body string
	// ID is a unique identifier of the resource in the response
	ID []string
	// Version is the version of the resource in the response
	Version []string
	// Port is the port of the resource in the response
	Port []string
	// Code is the response code
	Code []string
	// Host is the host returned by the response
	Host []string
}

// IsHTTPOk returns true if the response code was 200
func (r *ClientResponse) IsHTTPOk() bool {
	return len(r.Code) > 0 && r.Code[0] == httpOK
}

// accessLogs collects test expectations for access logs
type accessLogs struct {
	mu sync.Mutex

	// logs is a mapping from app name to requests
	logs map[string][]request
}

type request struct {
	id   string
	desc string
}

// NewAccessLogs creates an initialized accessLogs instance.
func newAccessLogs() *accessLogs {
	return &accessLogs{
		logs: make(map[string][]request),
	}
}

// add an access log entry for an app
func (a *accessLogs) add(app, id, desc string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logs[app] = append(a.logs[app], request{id: id, desc: desc})
}

// CheckLogs verifies the logs against a deployment
func (a *accessLogs) checkLogs(t *testing.T) {
	a.mu.Lock()
	defer a.mu.Unlock()
	log.Infof("Checking pod logs for request IDs...")
	log.Debuga(a.logs)

	for app := range a.logs {
		pods := a.getAppPods(t, app)

		// Check the logs for this app.
		a.checkLog(t, app, pods)
	}
}

func (a *accessLogs) getAppPods(t *testing.T, app string) []string {
	if app == ingressAppName {
		// Ingress is uses the "istio" label, not an "app" label.
		pods, err := util.GetIngressPodNames(tc.Kube.Namespace, tc.Kube.KubeConfig)
		if err != nil {
			t.Fatal(err)
		}
		if len(pods) == 0 {
			t.Fatal("Missing ingress pod")
		}
		return pods
	}

	log.Infof("Checking log for app: %q", app)
	pods := tc.Kube.GetAppPods()[app]
	if len(pods) == 0 {
		t.Fatalf("Missing pods for app: %q", app)
	}

	return pods
}

func (a *accessLogs) checkLog(t *testing.T, app string, pods []string) {
	var container string
	if app == ingressAppName {
		container = ingressContainerName
	} else {
		container = inject.ProxyContainerName
	}

	runRetriableTest(t, app, defaultRetryBudget, func() error {
		// find all ids and counts
		// TODO: this can be optimized for many string submatching
		counts := make(map[string]int)
		for _, request := range a.logs[app] {
			counts[request.id] = counts[request.id] + 1
		}

		// Concat the logs from all pods.
		var logs string
		for _, pod := range pods {
			// Retrieve the logs from the service container
			logs += util.GetPodLogs(tc.Kube.Namespace, pod, container, false, false, tc.Kube.KubeConfig)
		}

		for id, want := range counts {
			got := strings.Count(logs, id)
			if got < want {
				log.Errorf("Got %d for %s in logs of %s, want %d", got, id, app, want)
				// Do not dump the logs. Its virtually useless even if just one iteration fails.
				//log.Errorf("Log: %s", logs)
				return errAgain
			}
		}

		return nil
	})
}
