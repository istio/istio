//go:build integ

//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package pilot

import (
	"fmt"
	"strings"
	"testing"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/http/headers"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	cdeployment "istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/util/sets"
)

func TestProxyHeaders(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
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
			instances := deployment.New(t).
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
				// Envoy has no way to turn this off
				"x-forwarded-proto",
				// Metadata exchange: under discussion of how we can strip this, but for now there is no way
				"x-envoy-peer-metadata",
				"x-envoy-peer-metadata-id",
				// Tracing decorator. We may consider disabling this if tracing is off?
				"x-envoy-decorator-operation",
			)

			checkNoProxyHeaders := check.Each(func(response echoClient.Response) error {
				for k, v := range response.RequestHeaders {
					hn := strings.ToLower(k)
					if allowedClientHeaders.Contains(hn) {
						// This is allowed
						continue
					}
					if proxyHeaders.Contains(hn) || strings.HasPrefix(hn, "x-") {
						return fmt.Errorf("got unexpected proxy header: %v=%v", k, v)
					}
				}
				return nil
			})

			// Check request and responses have no proxy headers
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
				HTTP:    echo.HTTP{Headers: headers.New().WithHost(apps.External.All.Config().DefaultHostHeader).Build()},
				Scheme:  scheme.HTTP,
				Port:    ports.HTTP,
				Check:   check.And(check.OK(), checkNoProxyMetaHeaders),
			})
		})
}

func TestXfccHeaders(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
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
			instances := deployment.New(t).
				WithConfig(cfg).
				BuildOrFail(t)
			instance := instances[0]
			proxyHeaders := sets.New(
				"server",
				"x-request-id",
			)

			allowedClientHeaders := sets.New(
				// Envoy has no way to turn this off
				"x-forwarded-proto",
				// Metadata exchange: under discussion of how we can strip this, but for now there is no way
				"x-envoy-peer-metadata",
				"x-envoy-peer-metadata-id",
				// Tracing decorator. We may consider disabling this if tracing is off?
				"x-envoy-decorator-operation",
				"x-forwarded-client-cert",
			)

			checkNoProxyHeaders := check.Each(func(response echoClient.Response) error {
				for k, v := range response.RequestHeaders {
					hn := strings.ToLower(k)
					if allowedClientHeaders.Contains(hn) {
						// This is allowed
						continue
					}
					if proxyHeaders.Contains(hn) || strings.HasPrefix(hn, "x-") {
						return fmt.Errorf("got unexpected proxy header: %v=%v", k, v)
					}
					// Validate XFCC header as subject and cert.
					if strings.HasPrefix(hn, "x-forwarded-client-cert") {
						xfcc := v[0]
						if !strings.Contains(xfcc, "subject") || !strings.Contains(xfcc, "cert") {
							return fmt.Errorf("got unexpected XFCC header: %v=%v", k, v)
						}
					}
				}
				return nil
			})

			// Check request and responses have no proxy headers
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
				HTTP:    echo.HTTP{Headers: headers.New().WithHost(apps.External.All.Config().DefaultHostHeader).Build()},
				Scheme:  scheme.HTTP,
				Port:    ports.HTTP,
				Check:   check.And(check.OK(), checkNoProxyMetaHeaders),
			})
		})
}
