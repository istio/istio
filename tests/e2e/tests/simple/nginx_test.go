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

package simple

import (
	"bufio"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	nginxIngressYaml   = "tests/e2e/tests/simple/testdata/nginx/ingress.yaml"
	nginxYamlTmpl      = "tests/e2e/tests/simple/testdata/nginx/nginx.yaml.tmpl"
	appYamlTmpl        = "tests/e2e/tests/simple/testdata/nginx/app.yaml.tmpl"
	httpOK             = "200"
	nginxServiceName   = "ingress-nginx"
	defaultRetryBudget = 10
	retryDelay         = time.Second
)

var (
	idRegex      = regexp.MustCompile("(?i)X-Request-Id=(.*)")
	versionRegex = regexp.MustCompile("ServiceVersion=(.*)")
	portRegex    = regexp.MustCompile("ServicePort=(.*)")
	codeRegex    = regexp.MustCompile("StatusCode=(.*)")
	errAgain     = errors.New("try again")
)

func TestNginx(t *testing.T) {
	apps := getApps(getKubeMasterCIDROrFail(t))
	defer func() {
		for _, app := range apps {
			_ = tc.Kube.AppManager.UndeployApp(&app)
		}
	}()
	for _, app := range apps {
		if err := tc.Kube.AppManager.DeployApp(&app); err != nil {
			t.Fatalf("Failed to deploy app %s: %v", app.AppYamlTemplate, err)
		}
	}
	if err := tc.Kube.AppManager.CheckDeployments(); err != nil {
		t.Fatal("Failed waiting for apps to start")
	}

	// Using t as the origin of the request, since it doesn't have a sidecar. If we send a request to the ingress
	// service, but with the Host of "a", the sidecar (i.e. Envoy) will figure it out and send the request directly
	// to "a". So to prevent that, we make the request from a pod without a sidecar.
	src := "t"
	dest := "a"
	url := fmt.Sprintf("http://%s/%s", nginxServiceName, src)

	runRetriableTest(t, "Reachable", defaultRetryBudget, func() error {
		hostHeader := fmt.Sprintf("-key Host -val %s", dest)
		resp := tc.SendClientRequest(src, url, 1, hostHeader)
		if !resp.IsHTTPOk() {
			return errAgain
		}

		// Scan the response and verify that X-Real-Ip is not set to localhost. This confirms that inbound traffic was
		// not redirected through Envoy.
		scanner := bufio.NewScanner(strings.NewReader(resp.Body))
		pattern := regexp.MustCompile(".* x-real-ip=(.*)")
		for scanner.Scan() {
			line := strings.ToLower(scanner.Text())
			submatch := pattern.FindStringSubmatch(line)
			if submatch != nil {
				realIP := submatch[0]
				if realIP == "127.0.0.1" {
					t.Fatalf("Incorrect value for X-Real-Ip on response line: %s", line)
				}
				// Success
				return nil
			}
		}

		t.Fatal("Response did not contain the X-Real-Ip header")
		return nil
	})
}

func getApps(kubeMasterCIDR string) []framework.App {
	return []framework.App{
		getApp("a", "a", 8080, 80, 9090, 90, 7070, 70, "v1", true, false),
		getApp("t", "t", 8080, 80, 9090, 90, 7070, 70, "unversioned", false, false),
		{
			AppYamlTemplate: util.GetResourcePath(nginxYamlTmpl),
			Template: map[string]string{
				//"ProxyHub":        tc.Kube.ProxyHub(),
				//"ProxyTag":        tc.Kube.ProxyTag(),
				//"IstioNamespace":  tc.Kube.IstioSystemNamespace(),
				//"ImagePullPolicy": tc.imagePullPolicy(),
				"KubeMasterCIDR": kubeMasterCIDR,
				"Namespace":      tc.Kube.Namespace,
			},
			KubeInject: true,
		},
		{
			AppYamlTemplate: util.GetResourcePath(nginxIngressYaml),
			Template:        nil,
			KubeInject:      false, // No need to inject since it will have no effect.
		},
	}
}

func getApp(deploymentName, serviceName string, port1, port2, port3, port4, port5, port6 int, version string, injectProxy, perServiceAuth bool) framework.App {
	// TODO(nmittler): Consul does not support management ports ... should we support other registries?
	healthPort := "true"

	// Return the config.
	return framework.App{
		AppYamlTemplate: util.GetResourcePath(appYamlTmpl),
		KubeInject:      injectProxy,
		Template: map[string]string{
			"Hub":             tc.Kube.PilotHub(),
			"Tag":             tc.Kube.PilotTag(),
			"service":         serviceName,
			"perServiceAuth":  strconv.FormatBool(perServiceAuth),
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
			"ImagePullPolicy": tc.imagePullPolicy(),
		},
	}
}

func (t *testConfig) imagePullPolicy() string {
	imagePullPolicy := tc.Kube.ImagePullPolicy()
	if imagePullPolicy == "" {
		imagePullPolicy = "IfNotPresent"
	}
	return imagePullPolicy
}

func getKubeMasterCIDROrFail(t *testing.T) string {
	ip, err := util.GetKubeMasterIP()
	if err != nil {
		t.Fatalf("Unable to retrieve Kube Master IP: %v", err)
		return ""
	}
	subnet, err := util.GetClusterSubnet()
	if err != nil {
		t.Fatalf("Unable to determine cluster subnet: %v", err)
	}
	return fmt.Sprintf("%s/%s", ip, subnet)
}

// SendClientRequest makes a request from inside the specified k8s container.
func (t *testConfig) SendClientRequest(app, url string, count int, extra string) ClientResponse {
	out := ClientResponse{}

	pods := t.Kube.GetAppPods(framework.PrimaryCluster)[app]
	if len(pods) == 0 {
		log.Errorf("Missing pod names for app %q", app)
		return out
	}

	pod := pods[0]
	cmd := fmt.Sprintf("client -url %s -count %d %s", url, count, extra)
	request, err := util.PodExec(t.Kube.Namespace, pod, "app", cmd, true, t.Kube.KubeConfig)
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
}

// IsHTTPOk returns true if the response code was 200
func (r *ClientResponse) IsHTTPOk() bool {
	return len(r.Code) > 0 && r.Code[0] == httpOK
}

// runRetriableTest runs the given test function the provided number of times.
func runRetriableTest(t *testing.T, testName string, retries int, f func() error) {
	t.Run(testName, func(t *testing.T) {
		// Run all request tests in parallel.
		// TODO(nmittler): t.Parallel()

		remaining := retries
		var errs error
		for {
			// Call the test function.
			remaining--
			err := f()
			if err == nil {
				// Test succeeded, we're done here.
				return
			}

			attempt := retries - remaining
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("attempt %d", attempt)))
			log.Infof("attempt #%d failed with %v", attempt, err)

			if remaining == 0 {
				// We're out of retries - fail the test now.
				t.Fatal(errs)
			}

			// Wait for a bit before retrying.
			retries--
			time.Sleep(retryDelay)
		}
	})
}
