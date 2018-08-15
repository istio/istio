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
	util2 "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	defaultRetryBudget      = 10
	retryDelay              = time.Second
	httpOK                  = "200"
	ingressAppName          = "ingress"
	ingressContainerName    = "ingress"
	defaultPropagationDelay = 5 * time.Second
	primaryCluster          = framework.PrimaryCluster
)

var (
	tc = &testConfig{
		Ingress: true,
		Egress:  true,
	}

	errAgain     = errors.New("try again")
	idRegex      = regexp.MustCompile("(?i)X-Request-Id=(.*)")
	versionRegex = regexp.MustCompile("ServiceVersion=(.*)")
	portRegex    = regexp.MustCompile("ServicePort=(.*)")
	codeRegex    = regexp.MustCompile("StatusCode=(.*)")
	hostRegex    = regexp.MustCompile("Host=(.*)")
)

func init() {
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

func TestCoreDumpGenerated(t *testing.T) {
	if os.Getenv("MINIKUBE") == "true" {
		t.Skipf("Skipping %s in minikube environment", t.Name())
	}
	for _, e := range os.Environ() {
		t.Log(e)
	}
	// Simplest way to create out of process core file.
	crashContainer := "ingressgateway"
	crashProgPath := "/tmp/crashing_program"
	coreDir := "var/istio/proxy"
	script := `#!/bin/bash
kill -SIGSEGV $$
`
	err := ioutil.WriteFile(crashProgPath, []byte(script), 0755)
	if err != nil {
		t.Fatal(err)
	}

	ingressGatewayPod, err := getIngressGatewayPodName()
	if err != nil {
		t.Fatal(err)
	}

	if err := util2.CopyFilesToPod(crashContainer, ingressGatewayPod, tc.Kube.Namespace, crashProgPath, crashProgPath); err != nil {
		t.Fatalf("could not copy file to pod %s: %v", ingressGatewayPod, err)
	}

	out, err := util.PodExec(tc.Kube.Namespace, ingressGatewayPod, crashContainer, crashProgPath, false, "")
	if !strings.HasPrefix(out, "command terminated with exit code 139") {
		t.Fatalf("did not get expected crash error for %s in pod %s, got: %v", crashProgPath, ingressGatewayPod, err)
	}

	// No easy way to look for a specific core file.
	response, err := util.PodExec(tc.Kube.Namespace, ingressGatewayPod, crashContainer, "find "+coreDir, false, "")
	if !strings.Contains(response, "core.") {
		t.Fatalf("%s did not contain core file, contents: %s, err:%v", coreDir, response, err)
	}

	response, err = util.PodExec(tc.Kube.Namespace, ingressGatewayPod, crashContainer, "rm "+getCorefilename(response), false, "")
	if err != nil {
		t.Fatalf("could not remove core file, response:%s, err:%v", response, err)
	}
}

func getIngressGatewayPodName() (string, error) {
	label := "istio=ingressgateway"
	res, err := util.Shell("kubectl -n %s -l=%s get pods -o=jsonpath='{range .items[*]}{.metadata.name}{\" \"}{"+
		".metadata.labels.%s}{\"\\n\"}{end}'", tc.Kube.Namespace, label, label)
	if err != nil {
		return "", err
	}

	rv := strings.Split(res, "\n")
	if len(rv) < 2 {
		return "", fmt.Errorf("bad response for get ingressgateway: %s", res)
	}

	return strings.TrimSpace(rv[0]), nil
}

func getCorefilename(resp string) string {
	for _, line := range strings.Split(resp, "\n") {
		if strings.Contains(line, "core.") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func setTestConfig() error {
	cc, err := framework.NewCommonConfig("pilot_test")
	if err != nil {
		return err
	}
	tc.CommonConfig = cc

	tc.Kube.InstallAddons = true // zipkin is used

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
	tc.extraConfig = make(map[string]*deployableConfig)
	for cluster, kc := range tc.Kube.Clusters {
		tc.extraConfig[cluster] = &deployableConfig{
			Namespace: tc.Kube.Namespace,
			YamlFiles: []string{
				"testdata/external-wikipedia.yaml",
				"testdata/externalbin.yaml",
			},
			kubeconfig: kc,
		}
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
func runRetriableTest(t *testing.T, cluster, testName string, retries int, f func() error, errorFunc ...func()) {
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

type resource struct {
	// Kind of the resource
	Kind string
	// Name of the resource
	Name string
}

// deployableConfig is a collection of configs that are applied/deleted as a single unit.
type deployableConfig struct {
	Namespace string
	YamlFiles []string
	// List of resources must be removed during deployableConfig setup, and restored
	// during teardown. These resources must exist before deployableConfig setup runs, and should be
	// in the same namespace defined above. Typically, they are added by the default Istio installation
	// (e.g the default global authentication policy) and need to be modified for tests.
	Removes    []resource
	applied    []string
	removed    []string
	kubeconfig string
}

// Setup pushes the config and waits for it to propagate to all nodes in the cluster.
func (c *deployableConfig) Setup() error {
	c.removed = []string{}
	for _, r := range c.Removes {
		content, err := util.KubeGetYaml(c.Namespace, r.Kind, r.Name, c.kubeconfig)
		if err != nil {
			// Run the teardown function now and return
			_ = c.Teardown()
			return err
		}
		if err := util.KubeDeleteContents(c.Namespace, content, c.kubeconfig); err != nil {
			// Run the teardown function now and return
			_ = c.Teardown()
			return err
		}
		c.removed = append(c.removed, content)
	}
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
	// Restore configs that was removed
	for _, yaml := range c.removed {
		err = multierr.Append(err, util.KubeApplyContents(c.Namespace, yaml, c.kubeconfig))
	}
	c.applied = []string{}
	return err
}

func (c *deployableConfig) propagationDelay() time.Duration {
	// With multiple clusters, it takes more time to propagate.
	return defaultPropagationDelay * time.Duration(len(tc.Kube.Clusters))
}

type testConfig struct {
	*framework.CommonConfig
	AppDir      string
	Ingress     bool
	Egress      bool
	extraConfig map[string]*deployableConfig
}

// Setup initializes the test environment and waits for all pods to be in the running state.
func (t *testConfig) Setup() (err error) {
	// Deploy additional configuration.
	for _, ec := range t.extraConfig {
		err = ec.Setup()
		if err != nil {
			return
		}
	}

	// Wait for all the pods to be in the running state before starting tests.
	for cluster, kc := range t.Kube.Clusters {
		if err == nil && !util.CheckPodsRunning(t.Kube.Namespace, kc) {
			err = fmt.Errorf("can't get all pods running in %s cluster", cluster)
			break
		}
	}

	return
}

// Teardown shuts down the test environment.
func (t *testConfig) Teardown() (err error) {
	// Remove additional configuration.
	for _, ec := range t.extraConfig {
		e := ec.Teardown()
		if e != nil {
			err = multierr.Append(err, e)
		}
	}
	return
}

func getApps(tc *testConfig) []framework.App {
	return []framework.App{
		// deploy a healthy mix of apps, with and without proxy
		getApp("t", "t", 8080, 80, 9090, 90, 7070, 70, "unversioned", false, false, false),
		getApp("a", "a", 8080, 80, 9090, 90, 7070, 70, "v1", true, false, true),
		getApp("b", "b", 80, 8080, 90, 9090, 70, 7070, "unversioned", true, false, true),
		getApp("c-v1", "c", 80, 8080, 90, 9090, 70, 7070, "v1", true, false, true),
		getApp("c-v2", "c", 80, 8080, 90, 9090, 70, 7070, "v2", true, false, true),
		getApp("d", "d", 80, 8080, 90, 9090, 70, 7070, "per-svc-auth", true, false, true),
		getApp("headless", "headless", 80, 8080, 10090, 19090, 70, 7070, "unversioned", true, true, true),
		getStatefulSet("statefulset", 19090, true),
	}
}

func getApp(deploymentName, serviceName string, port1, port2, port3, port4, port5, port6 int,
	version string, injectProxy bool, headless bool, serviceAccount bool) framework.App {
	// TODO(nmittler): Consul does not support management ports ... should we support other registries?
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
			"headless":        strconv.FormatBool(headless),
			"serviceAccount":  strconv.FormatBool(serviceAccount),
			"healthPort":      healthPort,
			"ImagePullPolicy": tc.Kube.ImagePullPolicy(),
		},
		KubeInject: injectProxy,
	}
}

func getStatefulSet(service string, port int, injectProxy bool) framework.App {

	// Return the config.
	return framework.App{
		AppYamlTemplate: "testdata/statefulset.yaml.tmpl",
		Template: map[string]string{
			"Hub":             tc.Kube.PilotHub(),
			"Tag":             tc.Kube.PilotTag(),
			"service":         service,
			"port":            strconv.Itoa(port),
			"istioNamespace":  tc.Kube.Namespace,
			"injectProxy":     strconv.FormatBool(injectProxy),
			"ImagePullPolicy": tc.Kube.ImagePullPolicy(),
		},
		KubeInject: injectProxy,
	}
}

// ClientRequest makes a request from inside the specified k8s container.
func ClientRequest(cluster, app, url string, count int, extra string) ClientResponse {
	out := ClientResponse{}

	pods := tc.Kube.GetAppPods(cluster)[app]
	if len(pods) == 0 {
		log.Errorf("Missing pod names for app %q from %s cluster", app, cluster)
		return out
	}

	pod := pods[0]
	cmd := fmt.Sprintf("client -url %s -count %d %s", url, count, extra)
	request, err := util.PodExec(tc.Kube.Namespace, pod, "app", cmd, true, tc.Kube.Clusters[cluster])
	if err != nil {
		log.Errorf("client request error %v for %s in %s from %s cluster", err, url, app, cluster)
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

	// logs is a mapping from app name to requests for both primary and remote clusters
	logs map[string]map[string][]request
}

type request struct {
	id   string
	desc string
}

// NewAccessLogs creates an initialized accessLogs instance.
func newAccessLogs() *accessLogs {
	al := make(map[string]map[string][]request)
	for cluster := range tc.Kube.Clusters {
		al[cluster] = make(map[string][]request)
	}
	return &accessLogs{
		logs: al,
	}
}

// add an access log entry for an app
func (a *accessLogs) add(cluster, app, id, desc string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logs[cluster][app] = append(a.logs[cluster][app], request{id: id, desc: desc})
}

// CheckLogs verifies the logs against a deployment
func (a *accessLogs) checkLogs(t *testing.T) {
	a.mu.Lock()
	defer a.mu.Unlock()
	log.Infof("Checking pod logs for request IDs...")
	log.Debuga(a.logs)

	for cluster, apps := range a.logs {
		for app := range apps {
			pods := a.getAppPods(t, app)

			// Check the logs for this app.
			a.checkLog(t, cluster, app, pods)
		}
	}
}

func (a *accessLogs) getAppPods(t *testing.T, app string) map[string][]string {
	pods := make(map[string][]string)
	if app == ingressAppName {
		// Ingress uses the "istio" label, not an "app" label.
		ingressPods, err := util.GetIngressPodNames(tc.Kube.Namespace, tc.Kube.KubeConfig)
		if err != nil {
			t.Fatal(err)
		}
		if len(ingressPods) == 0 {
			t.Fatal("Missing ingress pod")
		}
		pods[primaryCluster] = ingressPods
		return pods
	}

	log.Infof("Checking log for app: %q", app)
	// Pods for the app needs to be obtained from all the clusters.
	for cluster := range a.logs {
		tmpPods := tc.Kube.GetAppPods(cluster)[app]
		if len(tmpPods) == 0 {
			t.Fatalf("Missing pods for app: %q from %s cluster", app, cluster)
		}
		pods[cluster] = tmpPods
	}

	return pods
}

func (a *accessLogs) checkLog(t *testing.T, cluster, app string, pods map[string][]string) {
	var container string
	if app == ingressAppName {
		container = ingressContainerName
	} else {
		container = inject.ProxyContainerName
	}

	runRetriableTest(t, cluster, app, defaultRetryBudget, func() error {
		// find all ids and counts
		// TODO: this can be optimized for many string submatching
		counts := make(map[string]int)
		for _, request := range a.logs[cluster][app] {
			counts[request.id] = counts[request.id] + 1
		}

		// Concat the logs from all pods.
		var logs string
		for c, cPods := range pods {
			for _, pod := range cPods {
				// Retrieve the logs from the service container
				logs += util.GetPodLogs(tc.Kube.Namespace, pod, container, false, false, tc.Kube.Clusters[c])
			}
		}

		for id, want := range counts {
			got := strings.Count(logs, id)
			if got < want {
				log.Errorf("Got %d for %s in logs of %s from %s cluster, want %d", got, id, app, cluster, want)
				// Do not dump the logs. Its virtually useless even if just one iteration fails.
				//log.Errorf("Log: %s", logs)
				return errAgain
			}
		}

		return nil
	})
}
