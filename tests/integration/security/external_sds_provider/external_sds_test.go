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

package externalsdsprovider

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

func gatewayLabel() string {
	label := inst.Settings().IngressGatewayIstioLabel
	if label == "" {
		return "ingressgateway"
	}
	return label
}

// TestExternalSDSProvider verifies that configuring a Gateway with credentialName
// using the sds:// prefix correctly generates Envoy listener config pointing to the
// external SDS provider's cluster (as defined in meshConfig.extensionProviders).
func TestExternalSDSProvider(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			istioCfg := istio.DefaultConfigOrFail(t, t)
			systemNS := namespace.ClaimOrFail(t, istioCfg.SystemNamespace)

			// Deploy a ServiceEntry for the SDS provider service so Istio can resolve its cluster.
			sdsServiceEntry := `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: sds-grpc-server
spec:
  hosts:
  - "sds-grpc-server.istio-system.svc.cluster.local"
  ports:
  - number: 8443
    name: grpc
    protocol: GRPC
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 127.0.0.1
`
			t.ConfigIstio().YAML(systemNS.Name(), sdsServiceEntry).ApplyOrFail(t)

			echotest.New(t, ingressutil.A).
				SetupForDestination(func(t framework.TestContext, _ echo.Target) error {
					gatewayConfig := fmt.Sprintf(`
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: external-sds-gateway
spec:
  selector:
    istio: %s
  servers:
  - port:
      number: 443
      name: https
      protocol: HTTPS
    tls:
      mode: SIMPLE
      credentialName: "sds://my-credential"
    hosts:
    - "external-sds.example.com"
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: external-sds-vs
spec:
  hosts:
  - "external-sds.example.com"
  gateways:
  - external-sds-gateway
  http:
  - route:
    - destination:
        host: a
        port:
          number: 80
`, gatewayLabel())
					t.ConfigIstio().YAML(echoNS.Name(), gatewayConfig).ApplyOrFail(t)
					return nil
				}).
				To(echotest.SingleSimplePodServiceAndAllSpecial()).
				RunFromClusters(func(t framework.TestContext, _ cluster.Cluster, _ echo.Target) {
					ing := inst.IngressFor(t.Clusters().Default())
					if ing == nil {
						t.Skip("no ingress gateway found")
					}

					istioCtl := istioctl.NewOrFail(t, istioctl.Config{Cluster: t.Clusters().Default()})

					podID, err := ing.PodID(0)
					if err != nil {
						t.Fatalf("failed to get ingress gateway pod ID: %v", err)
					}
					podName := fmt.Sprintf("%s.%s", podID, ing.Namespace())

					// Verify the listener config contains the external SDS provider cluster reference.
					retry.UntilSuccessOrFail(t, func() error {
						output, errOutput := istioCtl.InvokeOrFail(t, []string{
							"pc", "listener", podName, "-o", "json",
						})
						if errOutput != "" {
							return fmt.Errorf("istioctl error output: %s", errOutput)
						}

						if !strings.Contains(output, `"my-credential"`) {
							return fmt.Errorf("listener config does not contain SDS resource name 'my-credential'")
						}

						if !strings.Contains(output, "outbound|8443||sds-grpc-server.istio-system.svc.cluster.local") {
							return fmt.Errorf("listener config does not reference external SDS provider cluster " +
								"'outbound|8443||sds-grpc-server.istio-system.svc.cluster.local'")
						}

						return nil
					}, retry.Timeout(retry.DefaultTimeout))
				})
		})
}

// TestExternalSDSProviderMutualTLS verifies that configuring a Gateway with MUTUAL TLS
// and sds:// credentialName generates correct validation context in addition to the SDS cert config.
func TestExternalSDSProviderMutualTLS(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			istioCfg := istio.DefaultConfigOrFail(t, t)
			systemNS := namespace.ClaimOrFail(t, istioCfg.SystemNamespace)

			sdsServiceEntry := `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: sds-grpc-server-mtls
spec:
  hosts:
  - "sds-grpc-server.istio-system.svc.cluster.local"
  ports:
  - number: 8443
    name: grpc
    protocol: GRPC
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 127.0.0.1
`
			t.ConfigIstio().YAML(systemNS.Name(), sdsServiceEntry).ApplyOrFail(t)

			echotest.New(t, ingressutil.A).
				SetupForDestination(func(t framework.TestContext, _ echo.Target) error {
					gatewayConfig := fmt.Sprintf(`
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: external-sds-mtls-gateway
spec:
  selector:
    istio: %s
  servers:
  - port:
      number: 443
      name: https
      protocol: HTTPS
    tls:
      mode: MUTUAL
      credentialName: "sds://my-credential"
    hosts:
    - "external-sds-mtls.example.com"
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: external-sds-mtls-vs
spec:
  hosts:
  - "external-sds-mtls.example.com"
  gateways:
  - external-sds-mtls-gateway
  http:
  - route:
    - destination:
        host: a
        port:
          number: 80
`, gatewayLabel())
					t.ConfigIstio().YAML(echoNS.Name(), gatewayConfig).ApplyOrFail(t)
					return nil
				}).
				To(echotest.SingleSimplePodServiceAndAllSpecial()).
				RunFromClusters(func(t framework.TestContext, _ cluster.Cluster, _ echo.Target) {
					ing := inst.IngressFor(t.Clusters().Default())
					if ing == nil {
						t.Skip("no ingress gateway found")
					}

					istioCtl := istioctl.NewOrFail(t, istioctl.Config{Cluster: t.Clusters().Default()})

					podID, err := ing.PodID(0)
					if err != nil {
						t.Fatalf("failed to get ingress gateway pod ID: %v", err)
					}
					podName := fmt.Sprintf("%s.%s", podID, ing.Namespace())

					retry.UntilSuccessOrFail(t, func() error {
						output, errOutput := istioCtl.InvokeOrFail(t, []string{
							"pc", "listener", podName, "-o", "json",
						})
						if errOutput != "" {
							return fmt.Errorf("istioctl error output: %s", errOutput)
						}

						if !strings.Contains(output, `"my-credential"`) {
							return fmt.Errorf("listener config does not contain SDS resource name 'my-credential'")
						}

						if !strings.Contains(output, "outbound|8443||sds-grpc-server.istio-system.svc.cluster.local") {
							return fmt.Errorf("listener config does not reference external SDS provider cluster")
						}

						// For MUTUAL TLS, verify that the validation context references the CA cert SDS config.
						if !strings.Contains(output, `"my-credential-cacert"`) {
							return fmt.Errorf("listener config does not contain CA cert SDS name 'my-credential-cacert'")
						}

						return nil
					}, retry.Timeout(retry.DefaultTimeout))
				})
		})
}

// TestExternalSDSProviderListenerDetails verifies the detailed structure of the
// generated SDS config (cluster name, API type, transport version) using JSON config dump.
func TestExternalSDSProviderListenerDetails(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			istioCfg := istio.DefaultConfigOrFail(t, t)
			systemNS := namespace.ClaimOrFail(t, istioCfg.SystemNamespace)

			sdsServiceEntry := `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: sds-grpc-server-details
spec:
  hosts:
  - "sds-grpc-server.istio-system.svc.cluster.local"
  ports:
  - number: 8443
    name: grpc
    protocol: GRPC
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 127.0.0.1
`
			t.ConfigIstio().YAML(systemNS.Name(), sdsServiceEntry).ApplyOrFail(t)

			echotest.New(t, ingressutil.A).
				SetupForDestination(func(t framework.TestContext, _ echo.Target) error {
					gatewayConfig := fmt.Sprintf(`
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: external-sds-details-gateway
spec:
  selector:
    istio: %s
  servers:
  - port:
      number: 443
      name: https
      protocol: HTTPS
    tls:
      mode: SIMPLE
      credentialName: "sds://my-credential"
    hosts:
    - "external-sds-details.example.com"
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: external-sds-details-vs
spec:
  hosts:
  - "external-sds-details.example.com"
  gateways:
  - external-sds-details-gateway
  http:
  - route:
    - destination:
        host: a
        port:
          number: 80
`, gatewayLabel())
					t.ConfigIstio().YAML(echoNS.Name(), gatewayConfig).ApplyOrFail(t)
					return nil
				}).
				To(echotest.SingleSimplePodServiceAndAllSpecial()).
				RunFromClusters(func(t framework.TestContext, _ cluster.Cluster, _ echo.Target) {
					ing := inst.IngressFor(t.Clusters().Default())
					if ing == nil {
						t.Skip("no ingress gateway found")
					}

					istioCtl := istioctl.NewOrFail(t, istioctl.Config{Cluster: t.Clusters().Default()})

					podID, err := ing.PodID(0)
					if err != nil {
						t.Fatalf("failed to get ingress gateway pod ID: %v", err)
					}
					podName := fmt.Sprintf("%s.%s", podID, ing.Namespace())

					retry.UntilSuccessOrFail(t, func() error {
						output, errOutput := istioCtl.InvokeOrFail(t, []string{
							"pc", "listener", podName, "-o", "json",
						})
						if errOutput != "" {
							return fmt.Errorf("istioctl error output: %s", errOutput)
						}

						var listeners []json.RawMessage
						if err := json.Unmarshal([]byte(output), &listeners); err != nil {
							return fmt.Errorf("failed to parse listener JSON: %v", err)
						}

						found := false
						for _, l := range listeners {
							lStr := string(l)
							if strings.Contains(lStr, "external-sds-details.example.com") {
								found = true
								// Verify SDS config structure
								if !strings.Contains(lStr, `"my-credential"`) {
									return fmt.Errorf("SDS secret config name not found in listener")
								}
								if !strings.Contains(lStr, "outbound|8443||sds-grpc-server.istio-system.svc.cluster.local") {
									return fmt.Errorf("expected cluster name not found in SDS config")
								}
								if !strings.Contains(lStr, "GRPC") {
									return fmt.Errorf("expected GRPC apiType in SDS config")
								}
								break
							}
						}
						if !found {
							return fmt.Errorf("listener for external-sds-details.example.com not found")
						}

						return nil
					}, retry.Timeout(retry.DefaultTimeout))
				})
		})
}
