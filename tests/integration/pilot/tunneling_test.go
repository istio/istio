//go:build integ

// Copyright Istio Authors
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
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/forwardproxy"
)

const (
	forwardProxyConfigMapFile    = "testdata/forward-proxy/configmap.tmpl.yaml"
	forwardProxyServiceFile      = "testdata/forward-proxy/service.tmpl.yaml"
	tunnelingDestinationRuleFile = "testdata/tunneling/destination-rule.tmpl.yaml"
)

type tunnelingTestCase struct {
	// configDir is a directory with Istio configuration files for a particular test case
	configDir string
}

type testRequestSpec struct {
	protocol protocol.Instance
	port     echo.Port
}

var forwardProxyConfigurations = []forwardproxy.ListenerSettings{
	{
		Port:        3128,
		HTTPVersion: forwardproxy.HTTP1,
		TLSEnabled:  false,
	},
	{
		Port:        4128,
		HTTPVersion: forwardproxy.HTTP1,
		TLSEnabled:  true,
	},
	{
		Port:        5128,
		HTTPVersion: forwardproxy.HTTP2,
		TLSEnabled:  false,
	},
	{
		Port:        6128,
		HTTPVersion: forwardproxy.HTTP2,
		TLSEnabled:  true,
	},
}

var requestsSpec = []testRequestSpec{
	{
		protocol: protocol.HTTP,
		port:     ports.TCPForHTTP,
	},
	{
		protocol: protocol.HTTPS,
		port:     ports.HTTPS,
	},
}

var testCases = []tunnelingTestCase{
	{
		configDir: "sidecar",
	},
	{
		configDir: "gateway/tcp",
	},
	{
		configDir: "gateway/tls/istio-mutual",
	},
	{
		configDir: "gateway/tls/passthrough",
	},
}

func TestTunnelingOutboundTraffic(t *testing.T) {
	framework.
		NewTest(t).
		RequireIstioVersion("1.15.0").
		Run(func(ctx framework.TestContext) {
			meshNs := apps.A.NamespaceName()
			externalNs := apps.External.Namespace.Name()

			applyForwardProxyConfigMaps(ctx, externalNs)
			ctx.ConfigIstio().EvalFile(externalNs, map[string]any{
				"OpenShift": ctx.Settings().OpenShift,
			}, "testdata/external-forward-proxy-deployment.yaml").ApplyOrFail(ctx)
			applyForwardProxyService(ctx, externalNs)
			externalForwardProxyIPs, err := i.PodIPsFor(ctx.Clusters().Default(), externalNs, "app=external-forward-proxy")
			if err != nil {
				t.Fatalf("error getting external forward proxy ips: %v", err)
			}

			for _, proxyConfig := range forwardProxyConfigurations {
				templateParams := map[string]any{
					"externalNamespace":             externalNs,
					"forwardProxyPort":              proxyConfig.Port,
					"tlsEnabled":                    proxyConfig.TLSEnabled,
					"externalSvcTcpPort":            ports.TCPForHTTP.ServicePort,
					"externalSvcTlsPort":            ports.HTTPS.ServicePort,
					"EgressGatewayIstioLabel":       i.Settings().EgressGatewayIstioLabel,
					"EgressGatewayServiceName":      i.Settings().EgressGatewayServiceName,
					"EgressGatewayServiceNamespace": i.Settings().EgressGatewayServiceNamespace,
				}
				ctx.ConfigIstio().EvalFile(externalNs, templateParams, tunnelingDestinationRuleFile).ApplyOrFail(ctx)

				for _, tc := range testCases {
					for _, file := range listFilesInDirectory(ctx, tc.configDir) {
						ctx.ConfigIstio().EvalFile(meshNs, templateParams, file).ApplyOrFail(ctx)
					}

					for _, spec := range requestsSpec {
						testName := fmt.Sprintf("%s/%s/%s/%s-request",
							proxyConfig.HTTPVersion, proxyConfig.TLSEnabledStr(), tc.configDir, spec.protocol)
						ctx.NewSubTest(testName).Run(func(ctx framework.TestContext) {
							// requests will fail until istio-proxy gets the Envoy configuration from istiod, so retries are necessary
							retry.UntilSuccessOrFail(ctx, func() error {
								client := apps.A[0]
								target := apps.External.All[0]
								if err := testConnectivity(client, target, spec.protocol, spec.port, testName); err != nil {
									return err
								}
								if err := verifyThatRequestWasTunneled(target, externalForwardProxyIPs, testName); err != nil {
									return err
								}
								return nil
							}, retry.Timeout(10*time.Second))
						})
					}

					for _, file := range listFilesInDirectory(ctx, tc.configDir) {
						ctx.ConfigIstio().EvalFile(meshNs, templateParams, file).DeleteOrFail(ctx)
					}

					// Make sure that configuration changes were pushed to istio-proxies.
					// Otherwise, test results could be false-positive,
					// because subsequent test cases could work thanks to previous configurations.

					waitUntilTunnelingConfigurationIsRemovedOrFail(ctx, meshNs, i.Settings().EgressGatewayServiceNamespace, i.Settings().EgressGatewayServiceName)
				}

				ctx.ConfigIstio().EvalFile(externalNs, templateParams, tunnelingDestinationRuleFile).DeleteOrFail(ctx)
			}
		})
}

func testConnectivity(from, to echo.Instance, p protocol.Instance, port echo.Port, testName string) error {
	res, err := from.Call(echo.CallOptions{
		Address: to.ClusterLocalFQDN(),
		Port: echo.Port{
			Protocol:    p,
			ServicePort: port.ServicePort,
		},
		HTTP: echo.HTTP{
			Path: "/" + testName,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to request to external service: %s", err)
	}
	if res.Responses[0].Code != "200" {
		return fmt.Errorf("expected to get 200 status code, got: %s", res.Responses[0].Code)
	}
	return nil
}

func verifyThatRequestWasTunneled(target echo.Instance, expectedSourceIPs []corev1.PodIP, expectedPath string) error {
	workloads, err := target.Workloads()
	if err != nil {
		return fmt.Errorf("failed to get workloads of %s: %s", target.ServiceName(), err)
	}
	var logs strings.Builder
	for _, w := range workloads {
		workloadLogs, err := w.Logs()
		if err != nil {
			return fmt.Errorf("failed to get logs of workload %s: %s", w.PodName(), err)
		}
		logs.WriteString(workloadLogs)
	}

	expectedTunnelLogFound := false
	for _, expectedSourceIP := range expectedSourceIPs {
		expectedLog := fmt.Sprintf("remoteAddr=%s method=GET url=/%s", expectedSourceIP.IP, expectedPath)
		if strings.Contains(logs.String(), expectedLog) {
			expectedTunnelLogFound = true
			break
		}
	}
	if !expectedTunnelLogFound {
		return fmt.Errorf("failed to find expected tunnel log in logs of %s", target.ServiceName())
	}
	return nil
}

func applyForwardProxyConfigMaps(ctx framework.TestContext, externalNs string) {
	bootstrapYaml, err := forwardproxy.GenerateForwardProxyBootstrapConfig(forwardProxyConfigurations)
	if err != nil {
		ctx.Fatalf("failed to generate bootstrap configuration for external-forward-proxy: %s", err)
	}

	subject := fmt.Sprintf("external-forward-proxy.%s.svc.cluster.local", externalNs)
	key, crt, err := forwardproxy.GenerateKeyAndCertificate(subject, ctx.TempDir())
	if err != nil {
		ctx.Fatalf("failed to generate private key and certificate: %s", err)
	}

	templateParams := map[string]any{
		"envoyYaml": bootstrapYaml,
		"keyPem":    key,
		"certPem":   crt,
	}
	ctx.ConfigIstio().EvalFile(externalNs, templateParams, forwardProxyConfigMapFile).ApplyOrFail(ctx)
}

func applyForwardProxyService(ctx framework.TestContext, externalNs string) {
	var servicePorts []corev1.ServicePort
	for i, cfg := range forwardProxyConfigurations {
		servicePorts = append(servicePorts, corev1.ServicePort{
			Name:       fmt.Sprintf("%s-%d", selectPortName(cfg.HTTPVersion), i),
			Port:       int32(cfg.Port),
			TargetPort: intstr.FromInt32(int32(cfg.Port)),
		})
	}
	templateParams := map[string]any{
		"ports": servicePorts,
	}
	ctx.ConfigIstio().EvalFile(externalNs, templateParams, forwardProxyServiceFile).ApplyOrFail(ctx)
}

func listFilesInDirectory(ctx framework.TestContext, dir string) []string {
	files, err := os.ReadDir("testdata/tunneling/" + dir)
	if err != nil {
		ctx.Fatalf("failed to read files in directory: %s", err)
	}
	filesList := make([]string, 0, len(files))
	for _, file := range files {
		filesList = append(filesList, fmt.Sprintf("testdata/tunneling/%s/%s", dir, file.Name()))
	}
	return filesList
}

func selectPortName(httpVersion string) string {
	if httpVersion == forwardproxy.HTTP1 {
		return "http-connect"
	}
	return "http2-connect"
}

func getPodName(ctx framework.TestContext, ns, appSelector string) string {
	return getPodStringProperty(ctx, ns, appSelector, func(pod corev1.Pod) string {
		return pod.Name
	})
}

func getPodStringProperty(ctx framework.TestContext, ns, selector string, getPodProperty func(pod corev1.Pod) string) string {
	var podProperty string
	kubeClient := ctx.Clusters().Default()
	retry.UntilSuccessOrFail(ctx, func() error {
		pods, err := kubeClient.PodsForSelector(context.TODO(), ns, fmt.Sprintf("app=%s", selector))
		if err != nil {
			return fmt.Errorf("failed to get pods for selector app=%s: %v", selector, err)
		}
		if len(pods.Items) == 0 {
			return fmt.Errorf("no pods for selector app=%s", selector)
		}
		podProperty = getPodProperty(pods.Items[0])
		return nil
	}, retry.Timeout(30*time.Second))
	return podProperty
}

func waitUntilTunnelingConfigurationIsRemovedOrFail(ctx framework.TestContext, meshNs string, egressNs string, egressLabel string) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		waitForTunnelingRemovedOrFail(ctx, meshNs, "a")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		waitForTunnelingRemovedOrFail(ctx, egressNs, egressLabel)
	}()
	wg.Wait()
}

func waitForTunnelingRemovedOrFail(ctx framework.TestContext, ns, app string) {
	istioCtl := istioctl.NewOrFail(ctx, istioctl.Config{Cluster: ctx.Clusters().Default()})
	podName := getPodName(ctx, ns, app)
	args := []string{"proxy-config", "listeners", "-n", ns, podName, "-o", "json"}
	retry.UntilSuccessOrFail(ctx, func() error {
		out, _, err := istioCtl.Invoke(args)
		if err != nil {
			return fmt.Errorf("failed to get listeners of %s/%s: %s", app, ns, err)
		}
		if strings.Contains(out, "tunnelingConfig") {
			return fmt.Errorf("tunnelingConfig was not removed from istio-proxy configuration in %s/%s", app, ns)
		}
		return nil
	}, retry.Timeout(10*time.Second))
}
