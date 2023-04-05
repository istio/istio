//go:build integ
// +build integ

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

package multiplecontrolplanes

import (
	"fmt"
	"net/http"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
)

var (
	// Istio System Namespaces
	userGroup1NS namespace.Instance
	userGroup2NS namespace.Instance

	// Application Namespaces.
	// echo1NS is under userGroup1NS controlplane and echo2NS and echo3NS are under userGroup2NS controlplane
	echo1NS namespace.Instance
	echo2NS namespace.Instance
	echo3NS namespace.Instance
	apps    deployment.Echos
)

// TestMain defines the entrypoint for multiple controlplane tests using revisions and discoverySelectors.
func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		// Requires two CPs with specific names to be configured.
		Label(label.CustomSetup).
		SetupParallel(
			namespace.Setup(&userGroup1NS, namespace.Config{Prefix: "usergroup-1", Labels: map[string]string{"usergroup": "usergroup-1"}}),
			namespace.Setup(&userGroup2NS, namespace.Config{Prefix: "usergroup-2", Labels: map[string]string{"usergroup": "usergroup-2"}})).
		Setup(istio.Setup(nil, func(_ resource.Context, cfg *istio.Config) {
			cfg.Values["global.istioNamespace"] = userGroup1NS.Name()
			cfg.SystemNamespace = userGroup1NS.Name()
			cfg.EastWestGatewayValues = fmt.Sprintf(`
values:
  global:
    istioNamespace: %s`, userGroup1NS.Name())
			cfg.ControlPlaneValues = fmt.Sprintf(`
namespace: %s
revision: usergroup-1
meshConfig:
  # REGISTRY_ONLY is used to verify custom resources scoping
  outboundTrafficPolicy:
    mode: REGISTRY_ONLY
  # CR scoping requires discoverySelectors to be configured
  discoverySelectors:
    - matchLabels:
        usergroup: usergroup-1
values:
  global:
    istioNamespace: %s
  pilot:
    env:
      # CR scoping is guarded by the feature flag
      ENABLE_ENHANCED_RESOURCE_SCOPING: true`,
				userGroup1NS.Name(), userGroup1NS.Name())
		})).
		Setup(istio.Setup(nil, func(_ resource.Context, cfg *istio.Config) {
			cfg.Values["global.istioNamespace"] = userGroup2NS.Name()
			cfg.SystemNamespace = userGroup2NS.Name()
			cfg.EastWestGatewayValues = fmt.Sprintf(`
values:
  global:
    istioNamespace: %s`, userGroup2NS.Name())
			cfg.ControlPlaneValues = fmt.Sprintf(`
namespace: %s
revision: usergroup-2
meshConfig:
  # REGISTRY_ONLY is used to verify custom resources scoping
  outboundTrafficPolicy:
    mode: REGISTRY_ONLY
  # CR scoping requires discoverySelectors to be configured
  discoverySelectors:
    - matchLabels:
        usergroup: usergroup-2
values:
  global:
    istioNamespace: %s
  pilot:
    env:
      # CR scoping is guarded by the feature flag
      ENABLE_ENHANCED_RESOURCE_SCOPING: true`,
				userGroup2NS.Name(), userGroup2NS.Name())
		})).
		SetupParallel(
			// application namespaces are labeled according to the required control plane ownership.
			namespace.Setup(&echo1NS, namespace.Config{Prefix: "echo1", Inject: true, Revision: "usergroup-1", Labels: map[string]string{"usergroup": "usergroup-1"}}),
			namespace.Setup(&echo2NS, namespace.Config{Prefix: "echo2", Inject: true, Revision: "usergroup-2", Labels: map[string]string{"usergroup": "usergroup-2"}}),
			namespace.Setup(&echo3NS, namespace.Config{Prefix: "echo3", Inject: true, Revision: "usergroup-2", Labels: map[string]string{"usergroup": "usergroup-2"}})).
		SetupParallel(
			deployment.Setup(&apps, deployment.Config{
				Namespaces: []namespace.Getter{
					namespace.Future(&echo1NS),
					namespace.Future(&echo2NS),
					namespace.Future(&echo3NS),
				},
			})).
		Run()
}

// TestMultiControlPlane sets up two distinct istio control planes and verify if resources and traffic are properly isolated
func TestMultiControlPlane(t *testing.T) {
	framework.NewTest(t).
		Features("installation.multiplecontrolplanes").
		Run(func(t framework.TestContext) {
			// configure peerauthentication per system namespace
			restrictUserGroups(t)

			testCases := []struct {
				name       string
				statusCode int
				from       echo.Instances
				to         echo.Instances
			}{
				{
					name:       "workloads within same usergroup can communicate, same namespace",
					statusCode: http.StatusOK,
					from:       apps.NS[0].A,
					to:         apps.NS[0].B,
				},
				{
					name:       "workloads within same usergroup can communicate, different namespaces",
					statusCode: http.StatusOK,
					from:       apps.NS[1].A,
					to:         apps.NS[2].B,
				},
				{
					name:       "workloads within different usergroups cannot communicate",
					statusCode: http.StatusBadGateway,
					from:       apps.NS[0].A,
					to:         apps.NS[1].B,
				},
			}

			for _, tc := range testCases {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					tc.from[0].CallOrFail(t, echo.CallOptions{
						To: tc.to,
						Port: echo.Port{
							Protocol:    protocol.HTTP,
							ServicePort: 80,
						},
						Check: check.And(
							check.ErrorOrStatus(tc.statusCode),
						),
					})
				})
			}
		})
}

// TestCustomResourceScoping sets up a CustomResource and verifies that the configuration is not leaked to namespaces owned by a different control plane
func TestCustomResourceScoping(t *testing.T) {
	framework.NewTest(t).
		Features("installation.multiplecontrolplanes").
		Run(func(t framework.TestContext) {
			// allow access to external service only for app-ns-2 namespace which is under usergroup-2
			allowSpecificExternalService(t, apps.NS[1].Namespace.Name())

			testCases := []struct {
				name       string
				statusCode int
				from       echo.Instances
			}{
				{
					name:       "workloads in non-SE configured usergroup cannot reach external service",
					statusCode: http.StatusBadGateway,
					from:       apps.NS[0].A,
				},
				{
					name:       "workloads in SE configured namespace can reach external service",
					statusCode: http.StatusOK,
					from:       apps.NS[1].A,
				},
				{
					name:       "workloads in non-SE configured namespace, but same usergroup can reach external service",
					statusCode: http.StatusOK,
					from:       apps.NS[2].A,
				},
			}
			for _, tc := range testCases {
				t.NewSubTestf(tc.name).Run(func(t framework.TestContext) {
					tc.from[0].CallOrFail(t, echo.CallOptions{
						Address: "httpbin.org",
						Port:    echo.Port{Name: "http", ServicePort: 80},
						Scheme:  scheme.HTTP,
						HTTP: echo.HTTP{
							Path: "/headers",
						},
						Check: check.And(
							check.ErrorOrStatus(tc.statusCode),
						),
					})
				})
			}
		})
}

func restrictUserGroups(t framework.TestContext) {
	for _, ns := range []string{userGroup1NS.Name(), userGroup2NS.Name()} {
		t.ConfigIstio().Eval(ns, map[string]any{
			"Namespace": ns,
		}, `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: "usergroup-peerauth"
  namespace: {{ .Namespace }}
spec:
  mtls:
    mode: STRICT
`).ApplyOrFail(t, apply.NoCleanup)
	}
}

func allowSpecificExternalService(t framework.TestContext, ns string) {
	t.ConfigIstio().Eval(ns, map[string]any{
		"Namespace": ns,
	}, `apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: httpbin-ext
  namespace: {{ .Namespace }}
spec:
  hosts:
  - httpbin.org
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: DNS
  location: MESH_EXTERNAL
`).ApplyOrFail(t, apply.NoCleanup)
}
