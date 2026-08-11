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

package common

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/forwardproxy"
)

// tunnelingDataPath returns an absolute path to a testdata file under
// tests/integration/pilot/testdata, so this function works regardless of
// which package (pilot or nftables) calls RunTunnelingOutboundTrafficTests.
func tunnelingDataPath(rel string) string {
	return filepath.Join(env.IstioSrc, "tests/integration/pilot/testdata", rel)
}

var tunnelingForwardProxyConfigurations = []forwardproxy.ListenerSettings{
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

type tunnelingRequestSpec struct {
	protocol protocol.Instance
	port     echo.Port
}

var tunnelingRequestsSpec = []tunnelingRequestSpec{
	{protocol: protocol.HTTP, port: ports.TCPForHTTP},
	{protocol: protocol.HTTPS, port: ports.HTTPS},
}

type tunnelingTestCase struct {
	configDir string
}

var tunnelingTestCases = []tunnelingTestCase{
	{configDir: "sidecar"},
	{configDir: "gateway/tcp"},
	{configDir: "gateway/tls/istio-mutual"},
	{configDir: "gateway/tls/passthrough"},
}

// RunTunnelingOutboundTrafficTests validates CONNECT tunneling of outbound HTTP/HTTPS
// through a forward proxy via sidecar and egress gateway.
func RunTunnelingOutboundTrafficTests(t framework.TestContext, i istio.Instance, apps deployment.SingleNamespaceView) {
	t.Helper()
	meshNs := apps.A.NamespaceName()
	externalNs := apps.External.Namespace.Name()

	applyTunnelingForwardProxyConfigMaps(t, externalNs)
	t.ConfigIstio().EvalFile(externalNs, map[string]any{
		"OpenShift": t.Settings().OpenShift,
	}, tunnelingDataPath("external-forward-proxy-deployment.yaml")).ApplyOrFail(t)
	applyTunnelingForwardProxyService(t, externalNs)
	externalForwardProxyIPs, err := i.PodIPsFor(t.Clusters().Default(), externalNs, "app=external-forward-proxy")
	if err != nil {
		t.Fatalf("error getting external forward proxy ips: %v", err)
	}

	for _, proxyConfig := range tunnelingForwardProxyConfigurations {
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
		t.ConfigIstio().EvalFile(externalNs, templateParams, tunnelingDataPath("tunneling/destination-rule.tmpl.yaml")).ApplyOrFail(t)

		for _, tc := range tunnelingTestCases {
			for _, file := range tunnelingListFilesInDirectory(t, tc.configDir) {
				t.ConfigIstio().EvalFile(meshNs, templateParams, file).ApplyOrFail(t)
			}

			for _, spec := range tunnelingRequestsSpec {
				testName := fmt.Sprintf("%s/%s/%s/%s-request",
					proxyConfig.HTTPVersion, proxyConfig.TLSEnabledStr(), tc.configDir, spec.protocol)
				t.NewSubTest(testName).Run(func(t framework.TestContext) {
					retry.UntilSuccessOrFail(t, func() error {
						client := apps.A[0]
						target := apps.External.All[0]
						if err := tunnelingTestConnectivity(client, target, spec.protocol, spec.port, testName); err != nil {
							return err
						}
						return tunnelingVerifyRequestWasTunneled(target, externalForwardProxyIPs, testName)
					}, retry.Timeout(10*time.Second))
				})
			}

			for _, file := range tunnelingListFilesInDirectory(t, tc.configDir) {
				t.ConfigIstio().EvalFile(meshNs, templateParams, file).DeleteOrFail(t)
			}

			waitUntilTunnelingConfigurationIsRemovedOrFail(t, meshNs, i.Settings().EgressGatewayServiceNamespace, i.Settings().EgressGatewayServiceName)
		}

		t.ConfigIstio().EvalFile(externalNs, templateParams, tunnelingDataPath("tunneling/destination-rule.tmpl.yaml")).DeleteOrFail(t)
	}
}

func tunnelingTestConnectivity(from, to echo.Instance, p protocol.Instance, port echo.Port, testName string) error {
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

func tunnelingVerifyRequestWasTunneled(target echo.Instance, expectedSourceIPs []corev1.PodIP, expectedPath string) error {
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

func applyTunnelingForwardProxyConfigMaps(t framework.TestContext, externalNs string) {
	bootstrapYaml, err := forwardproxy.GenerateForwardProxyBootstrapConfig(tunnelingForwardProxyConfigurations)
	if err != nil {
		t.Fatalf("failed to generate bootstrap configuration for external-forward-proxy: %s", err)
	}

	subject := fmt.Sprintf("external-forward-proxy.%s.svc.cluster.local", externalNs)
	key, crt, err := forwardproxy.GenerateKeyAndCertificate(subject, t.TempDir())
	if err != nil {
		t.Fatalf("failed to generate private key and certificate: %s", err)
	}

	templateParams := map[string]any{
		"envoyYaml": bootstrapYaml,
		"keyPem":    key,
		"certPem":   crt,
	}
	t.ConfigIstio().EvalFile(externalNs, templateParams, tunnelingDataPath("forward-proxy/configmap.tmpl.yaml")).ApplyOrFail(t)
}

func applyTunnelingForwardProxyService(t framework.TestContext, externalNs string) {
	var servicePorts []corev1.ServicePort
	for i, cfg := range tunnelingForwardProxyConfigurations {
		servicePorts = append(servicePorts, corev1.ServicePort{
			Name:       fmt.Sprintf("%s-%d", tunnelingSelectPortName(cfg.HTTPVersion), i),
			Port:       int32(cfg.Port),
			TargetPort: intstr.FromInt32(int32(cfg.Port)),
		})
	}
	templateParams := map[string]any{
		"ports": servicePorts,
	}
	t.ConfigIstio().EvalFile(externalNs, templateParams, tunnelingDataPath("forward-proxy/service.tmpl.yaml")).ApplyOrFail(t)
}

func tunnelingListFilesInDirectory(t framework.TestContext, dir string) []string {
	fullDir := tunnelingDataPath("tunneling/" + dir)
	files, err := os.ReadDir(fullDir)
	if err != nil {
		t.Fatalf("failed to read files in directory: %s", err)
	}
	filesList := make([]string, 0, len(files))
	for _, file := range files {
		filesList = append(filesList, tunnelingDataPath(fmt.Sprintf("tunneling/%s/%s", dir, file.Name())))
	}
	return filesList
}

func tunnelingSelectPortName(httpVersion string) string {
	if httpVersion == forwardproxy.HTTP1 {
		return "http-connect"
	}
	return "http2-connect"
}

func tunnelingGetPodName(t framework.TestContext, ns, appSelector string) string {
	return tunnelingGetPodStringProperty(t, ns, appSelector, func(pod corev1.Pod) string {
		return pod.Name
	})
}

func tunnelingGetPodStringProperty(t framework.TestContext, ns, selector string, getPodProperty func(pod corev1.Pod) string) string {
	var podProperty string
	kubeClient := t.Clusters().Default()
	retry.UntilSuccessOrFail(t, func() error {
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

func waitUntilTunnelingConfigurationIsRemovedOrFail(t framework.TestContext, meshNs string, egressNs string, egressLabel string) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		waitForTunnelingRemovedOrFail(t, meshNs, "a")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		waitForTunnelingRemovedOrFail(t, egressNs, egressLabel)
	}()
	wg.Wait()
}

func waitForTunnelingRemovedOrFail(t framework.TestContext, ns, app string) {
	istioCtl := istioctl.NewOrFail(t, istioctl.Config{Cluster: t.Clusters().Default()})
	podName := tunnelingGetPodName(t, ns, app)
	args := []string{"proxy-config", "listeners", "-n", ns, podName, "-o", "json"}
	retry.UntilSuccessOrFail(t, func() error {
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
