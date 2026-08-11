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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/stretchr/testify/assert"

	"istio.io/api/annotation"
	istiohttp "istio.io/istio/pkg/http/headers"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	cdeployment "istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	echodeployment "istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/util/sets"
)

// RunProxyHeaderTests validates that the proxyHeaders configuration (disabling server,
// requestId, attemptCount, envoyDebugHeaders, metadataExchangeHeaders) suppresses
// the expected proxy headers on both in-mesh and external calls.
func RunProxyHeaderTests(t framework.TestContext, apps cdeployment.SingleNamespaceView) {
	t.Helper()
	ns := namespace.NewOrFail(t, namespace.Config{Prefix: "proxy-headers", Inject: true})
	cfg := echo.Config{
		Namespace: ns,
		Ports:     ports.All(),
		Service:   "no-headers",
		Subsets: []echo.SubsetConfig{
			{
				Annotations: map[string]string{annotation.ProxyConfig.Name: `
tracing: {}
proxyHeaders:
  forwardedClientCert: SANITIZE
  server:
    disabled: true
  requestId:
    disabled: true
  attemptCount:
    disabled: true
  envoyDebugHeaders:
    disabled: true
  metadataExchangeHeaders:
    mode: IN_MESH`},
			},
		},
	}
	instances := echodeployment.New(t).
		WithConfig(cfg).
		BuildOrFail(t)
	instance := instances[0]
	proxyHeaders := sets.New(
		"server",
		"x-forwarded-client-cert",
		"x-request-id",
		"x-envoy-attempt-count",
	)
	allowedClientHeaders := sets.New(
		"x-forwarded-proto",
		"x-envoy-peer-metadata",
		"x-envoy-peer-metadata-id",
		"x-envoy-decorator-operation",
	)

	checkNoProxyHeaders := check.Each(func(response echoClient.Response) error {
		for k, v := range response.RequestHeaders {
			hn := strings.ToLower(k)
			if allowedClientHeaders.Contains(hn) {
				continue
			}
			if proxyHeaders.Contains(hn) || strings.HasPrefix(hn, "x-") {
				return fmt.Errorf("got unexpected proxy header: %v=%v", k, v)
			}
		}
		return nil
	})

	instance.CallOrFail(t, echo.CallOptions{
		To: apps.Naked,
		Port: echo.Port{
			Name: ports.HTTP.Name,
		},
		Check: check.And(check.OK(), checkNoProxyHeaders),
	})
	apps.Naked[0].CallOrFail(t, echo.CallOptions{
		To: instance,
		Port: echo.Port{
			Name: ports.HTTP.Name,
		},
		Check: check.And(check.OK(), checkNoProxyHeaders),
	})

	checkNoProxyMetaHeaders := check.Each(func(response echoClient.Response) error {
		for k, v := range response.RequestHeaders {
			hn := strings.ToLower(k)
			if strings.HasPrefix(hn, "x-envoy-peer-metadata") {
				return fmt.Errorf("got unexpected proxy header: %v=%v", k, v)
			}
		}
		return nil
	})

	cdeployment.DeployExternalServiceEntry(t.ConfigIstio(), ns, apps.External.Namespace, false).
		ApplyOrFail(t, apply.CleanupConditionally)
	instance.CallOrFail(t, echo.CallOptions{
		Address: apps.External.All[0].Address(),
		HTTP:    echo.HTTP{Headers: istiohttp.New().WithHost(apps.External.All.Config().DefaultHostHeader).Build()},
		Scheme:  scheme.HTTP,
		Port:    ports.HTTP,
		Check:   check.And(check.OK(), checkNoProxyMetaHeaders),
	})
}

// RunXfccHeaderTests validates that APPEND_FORWARD forwardedClientCert mode includes
// subject and cert details in the X-Forwarded-Client-Cert header on in-mesh calls.
func RunXfccHeaderTests(t framework.TestContext, apps cdeployment.SingleNamespaceView) {
	t.Helper()
	ns := namespace.NewOrFail(t, namespace.Config{Prefix: "proxy-headers", Inject: true})
	cfg := echo.Config{
		Namespace: ns,
		Ports:     ports.All(),
		Service:   "no-headers",
		Subsets: []echo.SubsetConfig{
			{
				Annotations: map[string]string{annotation.ProxyConfig.Name: `
tracing: {}
proxyHeaders:
  forwardedClientCert: APPEND_FORWARD
  setCurrentClientCertDetails:
    subject: true
    cert: true
  server:
    disabled: true
  requestId:
    disabled: true
  attemptCount:
    disabled: true
  envoyDebugHeaders:
    disabled: true
  metadataExchangeHeaders:
    mode: IN_MESH`},
			},
		},
	}
	instances := echodeployment.New(t).
		WithConfig(cfg).
		BuildOrFail(t)
	instance := instances[0]
	proxyHeaders := sets.New(
		"server",
		"x-request-id",
	)
	allowedClientHeaders := sets.New(
		"x-forwarded-proto",
		"x-envoy-peer-metadata",
		"x-envoy-peer-metadata-id",
		"x-envoy-decorator-operation",
		"x-forwarded-client-cert",
	)

	checkNoProxyHeaders := check.Each(func(response echoClient.Response) error {
		for k, v := range response.RequestHeaders {
			hn := strings.ToLower(k)
			if allowedClientHeaders.Contains(hn) {
				continue
			}
			if proxyHeaders.Contains(hn) || strings.HasPrefix(hn, "x-") {
				return fmt.Errorf("got unexpected proxy header: %v=%v", k, v)
			}
			if strings.HasPrefix(hn, "x-forwarded-client-cert") {
				xfcc := v[0]
				if !strings.Contains(xfcc, "subject") || !strings.Contains(xfcc, "cert") {
					return fmt.Errorf("got unexpected XFCC header: %v=%v", k, v)
				}
			}
		}
		return nil
	})

	instance.CallOrFail(t, echo.CallOptions{
		To: apps.Naked,
		Port: echo.Port{
			Name: ports.HTTP.Name,
		},
		Check: check.And(check.OK(), checkNoProxyHeaders),
	})
	apps.Naked[0].CallOrFail(t, echo.CallOptions{
		To: instance,
		Port: echo.Port{
			Name: ports.HTTP.Name,
		},
		Check: check.And(check.OK(), checkNoProxyHeaders),
	})

	checkNoProxyMetaHeaders := check.Each(func(response echoClient.Response) error {
		for k, v := range response.RequestHeaders {
			hn := strings.ToLower(k)
			if strings.HasPrefix(hn, "x-envoy-peer-metadata") {
				return fmt.Errorf("got unexpected proxy header: %v=%v", k, v)
			}
		}
		return nil
	})

	cdeployment.DeployExternalServiceEntry(t.ConfigIstio(), ns, apps.External.Namespace, false).
		ApplyOrFail(t, apply.CleanupConditionally)
	instance.CallOrFail(t, echo.CallOptions{
		Address: apps.External.All[0].Address(),
		HTTP:    echo.HTTP{Headers: istiohttp.New().WithHost(apps.External.All.Config().DefaultHostHeader).Build()},
		Scheme:  scheme.HTTP,
		Port:    ports.HTTP,
		Check:   check.And(check.OK(), checkNoProxyMetaHeaders),
	})
}

// RunPreserveHTTPHeaderCaseTests validates that when preserveHttp1HeaderCase is enabled,
// mixed-case custom HTTP/1.x headers are preserved end-to-end through the sidecar.
func RunPreserveHTTPHeaderCaseTests(t framework.TestContext) {
	t.Helper()
	ns := namespace.NewOrFail(t, namespace.Config{
		Prefix: "echo-test",
		Inject: true,
	})

	echos := echodeployment.New(t)
	echos.WithClusters(t.Clusters()...)
	echos.WithConfig(echo.Config{
		Service:   "client",
		Namespace: ns,
		Ports:     ports.All(),
	})
	echos.WithConfig(echo.Config{
		Service:   "server",
		Namespace: ns,
		Ports:     ports.All(),
		Subsets: []echo.SubsetConfig{
			{
				Annotations: map[string]string{
					annotation.ProxyConfig.Name: `
                proxyHeaders:
                  preserveHttp1HeaderCase: true`,
				},
			},
		},
	})
	workloads := echos.BuildOrFail(t)
	client := match.ServiceName(echo.NamespacedName{Name: "client", Namespace: ns}).GetMatches(workloads)
	server := match.ServiceName(echo.NamespacedName{Name: "server", Namespace: ns}).GetMatches(workloads)

	const customHeaderKey = "X-Custom-Header"
	const customHeaderValue = "CustomValue"

	client[0].CallOrFail(t, echo.CallOptions{
		To:   server[0],
		Port: ports.HTTP,
		HTTP: echo.HTTP{
			Path: "/test",
			Headers: map[string][]string{
				customHeaderKey: {customHeaderValue},
			},
		},
		Check: check.And(
			check.OK(),
			check.Each(func(response echoClient.Response) error {
				actualValues, ok := response.RequestHeaders[customHeaderKey]
				if !ok || len(actualValues) == 0 || actualValues[0] != customHeaderValue {
					return fmt.Errorf("expected header '%s' with value '%s', but got: %v",
						customHeaderKey, customHeaderValue, response.RequestHeaders)
				}
				return nil
			}),
		),
	})
}

// RunPreserveHTTPHeaderCaseConfigurationTests validates that the preserveHttp1HeaderCase
// annotation correctly sets the preserve_case formatter in Envoy cluster and listener config.
// This test is backend-agnostic (xDS config only, no traffic) and is provided here for
// completeness; it does not need to be added to the nftables suite.
func RunPreserveHTTPHeaderCaseConfigurationTests(t framework.TestContext) {
	t.Helper()
	ns := namespace.NewOrFail(t, namespace.Config{
		Prefix: "echo-test",
		Inject: true,
	})

	echos := echodeployment.New(t)
	echos.WithClusters(t.Clusters()...)
	echos.WithConfig(echo.Config{
		Service:   "client",
		Namespace: ns,
		Ports:     ports.All(),
	})
	echos.WithConfig(echo.Config{
		Service:   "server",
		Namespace: ns,
		Ports:     ports.All(),
		Subsets: []echo.SubsetConfig{
			{
				Annotations: map[string]string{
					annotation.ProxyConfig.Name: `
proxyHeaders:
  preserveHttp1HeaderCase: true`,
				},
			},
		},
	})
	workloads := echos.BuildOrFail(t)
	server := match.ServiceName(echo.NamespacedName{Name: "server", Namespace: ns}).GetMatches(workloads)

	serverPodName := server[0].WorkloadsOrFail(t)[0].PodName()
	output, _ := istioctl.NewOrFail(t, istioctl.Config{}).InvokeOrFail(t,
		[]string{"proxy-config", "cluster", serverPodName, "--namespace", ns.Name(), "-o", "json"})
	assert.Contains(t, output, `"name": "preserve_case"`, "preserve_case configuration not found in cluster")
	assert.Contains(t, output,
		`"@type": "type.googleapis.com/envoy.extensions.http.header_formatters.preserve_case.v3.PreserveCaseFormatterConfig"`,
		"preserve_case type configuration not found in cluster")

	clusters := []map[string]json.RawMessage{}
	assert.NoError(t, json.Unmarshal([]byte(output), &clusters), "failed to unmarshal clusters")
	for _, c := range clusters {
		if string(c["name"]) == "\"PassthroughCluster\"" {
			assert.Contains(t, string(c["typedExtensionProtocolOptions"]),
				`"@type": "type.googleapis.com/envoy.extensions.http.header_formatters.preserve_case.v3.PreserveCaseFormatterConfig"`,
				"preserve_case type configuration not found in passthrough cluster")
			break
		}
	}

	output, _ = istioctl.NewOrFail(t, istioctl.Config{}).InvokeOrFail(t,
		[]string{"proxy-config", "listener", serverPodName, "--namespace", ns.Name(), "-o", "json"})
	assert.Contains(t, output, `"name": "preserve_case"`, "preserve_case configuration not found in listener")
	assert.Contains(t, output,
		`"@type": "type.googleapis.com/envoy.extensions.http.header_formatters.preserve_case.v3.PreserveCaseFormatterConfig"`,
		"preserve_case type configuration not found in listener")
}
