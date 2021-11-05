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

package sdstlsutil

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path"
	"time"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)

const (
	CallsPerCluster = 5
	ServerSvc       = "server"
)

type EchoDeployments struct {
	All             echo.Instances
	Server          echo.Instance
	ClientNamespace namespace.Instance
	ServerNamespace namespace.Instance
}

type TestCase struct {
	Response        string
	CredentialToUse string
	Gateway         bool // true if the request is expected to be routed through gateway
}

func MustReadCert(t test.Failer, f string) string {
	b, err := os.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs/dns", f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}

// SetupEcho creates two namespaces client and server. It also brings up echo instances server and
// clients in respective namespaces. HTTP and HTTPS port on the server echo are set up. Egress Gateway is set up in the
// service namespace to handle egress for "external" calls.
func SetupEcho(t test.Failer, ctx resource.Context, apps *EchoDeployments) {
	apps.ClientNamespace = namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "client",
		Inject: true,
	})
	apps.ServerNamespace = namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "server",
		Inject: true,
	})
	cluster := ctx.Clusters().Default()
	var internalClient, internalVM, externalServer echo.Instance
	echos := echoboot.NewBuilder(ctx).
		With(&internalClient, echo.Config{
			Cluster:   cluster,
			Service:   "a",
			Namespace: apps.ClientNamespace,
			Ports:     []echo.Port{},
			Subsets: []echo.SubsetConfig{{
				Version: "v1",
			}},
		}).
		With(&internalVM, echo.Config{
			Cluster:   cluster,
			Service:   "vm",
			Namespace: apps.ClientNamespace,
			Ports:     []echo.Port{},
			Subsets: []echo.SubsetConfig{{
				Version: "v1",
			}},
			DeployAsVM: true,
		}).
		With(&externalServer, echo.Config{
			Cluster:   cluster,
			Service:   ServerSvc,
			Namespace: apps.ServerNamespace,
			Ports: []echo.Port{
				{
					// Plain HTTP port only used to route request to egress gateway
					Name:         "http",
					Protocol:     protocol.HTTP,
					ServicePort:  80,
					InstancePort: 8080,
				},
				{
					// HTTPS port
					Name:         "https",
					Protocol:     protocol.HTTPS,
					ServicePort:  443,
					InstancePort: 8443,
					TLS:          true,
				},
			},
			// Set up TLS certs on the server. This will make the server listen with these credentials.
			TLSSettings: &common.TLSSettings{
				// Echo has these test certs baked into the docker image
				RootCert:   MustReadCert(t, "root-cert.pem"),
				ClientCert: MustReadCert(t, "cert-chain.pem"),
				Key:        MustReadCert(t, "key.pem"),
				// Override hostname to match the SAN in the cert we are using
				Hostname: "server.default.svc",
			},
			Subsets: []echo.SubsetConfig{{
				Version:     "v1",
				Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
			}},
		}).
		BuildOrFail(t)

	apps.All = echos
	apps.Server = externalServer
	// Apply Egress Gateway for service namespace to originate external traffic
	createGateway(t, ctx, apps.ClientNamespace, apps.ServerNamespace)

	if err := WaitUntilNotCallable(internalClient, externalServer); err != nil {
		t.Fatalf("failed to apply sidecar, %v", err)
	}
}

const (
	Gateway = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: istio-egressgateway-sds
spec:
  selector:
    istio: egressgateway
  servers:
    - port:
        number: 443
        name: https-sds
        protocol: HTTPS
      hosts:
      - server.{{.ServerNamespace}}.svc.cluster.local
      tls:
        mode: ISTIO_MUTUAL
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: egressgateway-for-server-sds
spec:
  host: istio-egressgateway.istio-system.svc.cluster.local
  subsets:
  - name: server
    trafficPolicy:
      portLevelSettings:
      - port:
          number: 443
        tls:
          mode: ISTIO_MUTUAL
          sni: server.{{.ServerNamespace}}.svc.cluster.local
`
	VirtualService = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route-via-egressgateway-sds
spec:
  hosts:
    - server.{{.ServerNamespace}}.svc.cluster.local
  gateways:
    - istio-egressgateway-sds
    - mesh
  http:
    - match:
        - gateways:
            - mesh # from sidecars, route to egress gateway service
          port: 80
      route:
        - destination:
            host: istio-egressgateway.istio-system.svc.cluster.local
            subset: server
            port:
              number: 443
          weight: 100
    - match:
        - gateways:
            - istio-egressgateway-sds
          port: 443
      route:
        - destination:
            host: server.{{.ServerNamespace}}.svc.cluster.local
            port:
              number: 443
          weight: 100
      headers:
        request:
          add:
            handled-by-egress-gateway: "true"
`
)

// We want to test out TLS origination at Gateway, to do so traffic from client in client namespace is first
// routed to egress-gateway service in istio-system namespace and then from egress-gateway to server in server namespace.
// TLS origination at Gateway happens using DestinationRule with CredentialName reading k8s secret at the gateway proxy.
func createGateway(t test.Failer, ctx resource.Context, clientNamespace namespace.Instance, serverNamespace namespace.Instance) {
	tmplGateway, err := template.New("Gateway").Parse(Gateway)
	if err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	var bufGateway bytes.Buffer
	if err := tmplGateway.Execute(&bufGateway, map[string]string{"ServerNamespace": serverNamespace.Name()}); err != nil {
		t.Fatalf("failed to create template: %v", err)
	}
	if err := ctx.ConfigKube(ctx.Clusters().Default()).ApplyYAML(clientNamespace.Name(), bufGateway.String()); err != nil {
		t.Fatalf("failed to apply gateway: %v. template: %v", err, bufGateway.String())
	}

	// Hack:Must give some time to DR to propagate to all configs
	time.Sleep(time.Second * 5)

	tmplVS, err := template.New("VirtualService").Parse(VirtualService)
	if err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	var bufVS bytes.Buffer
	if err := tmplVS.Execute(&bufVS, map[string]string{"ServerNamespace": serverNamespace.Name()}); err != nil {
		t.Fatalf("failed to create template: %v", err)
	}
	if err := ctx.ConfigKube(ctx.Clusters().Default()).ApplyYAML(clientNamespace.Name(), bufVS.String()); err != nil {
		t.Fatalf("failed to apply gateway: %v. template: %v", err, bufVS.String())
	}
}

const (
	// Destination Rule configs
	DestinationRuleConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: originate-tls-for-server-sds-{{.CredentialName}}
spec:
  host: "server.{{.ServerNamespace}}.svc.cluster.local"
  trafficPolicy:
    portLevelSettings:
      - port:
          number: 443
        tls:
          mode: {{.Mode}}
          credentialName: {{.CredentialName}}
          sni: server.{{.ServerNamespace}}.svc.cluster.local
`
)

// Create the DestinationRule for TLS origination at Gateway by reading secret in istio-system namespace.
func CreateDestinationRule(t test.Failer, serverNamespace namespace.Instance,
	destinationRuleMode string, credentialName string) bytes.Buffer {
	destinationRuleToParse := DestinationRuleConfig

	tmpl, err := template.New("DestinationRule").Parse(destinationRuleToParse)
	if err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{
		"ServerNamespace": serverNamespace.Name(),
		"Mode":            destinationRuleMode, "CredentialName": credentialName,
	}); err != nil {
		t.Fatalf("failed to create template: %v", err)
	}
	return buf
}

// Wait for the server to NOT be callable by the client. This allows us to simulate external traffic.
// This essentially just waits for the Sidecar to be applied, without sleeping.
func WaitUntilNotCallable(c echo.Instance, dest echo.Instance) error {
	accept := func(cfg *envoyAdmin.ConfigDump) (bool, error) {
		validator := structpath.ForProto(cfg)
		for _, port := range dest.Config().Ports {
			clusterName := clusterName(dest, port)
			// Ensure that we have an outbound configuration for the target port.
			err := validator.NotExists("{.configs[*].dynamicActiveClusters[?(@.cluster.Name == '%s')]}", clusterName).Check()
			if err != nil {
				return false, err
			}
		}

		return true, nil
	}

	workloads, _ := c.Workloads()
	// Wait for the outbound config to be received by each workload from Pilot.
	for _, w := range workloads {
		if w.Sidecar() != nil {
			if err := w.Sidecar().WaitForConfig(accept, retry.Timeout(time.Second*10)); err != nil {
				return err
			}
		}
	}

	return nil
}

func clusterName(target echo.Instance, port echo.Port) string {
	cfg := target.Config()
	return fmt.Sprintf("outbound|%d||%s.%s.svc.%s", port.ServicePort, cfg.Service, cfg.Namespace.Name(), cfg.Domain)
}

func CallOpts(dest echo.Instance, host, name string, tc TestCase) echo.CallOptions {
	return echo.CallOptions{
		Target:   dest,
		Count:    CallsPerCluster,
		PortName: "http",
		Scheme:   scheme.HTTP,
		Headers: map[string][]string{
			"Host": {host},
		},
		Validator: echo.And(echo.ValidatorFunc(
			func(responses client.ParsedResponses, err error) error {
				if err != nil {
					return fmt.Errorf("request failed: %v", err)
				}
				for _, r := range responses {
					if r.Code != tc.Response {
						return fmt.Errorf("got code %s, expected %s", r.Code, tc.Response)
					}
				}
				for _, r := range responses {
					if _, f := r.RawResponse["Handled-By-Egress-Gateway"]; tc.Gateway && !f {
						return fmt.Errorf("expected to be handled by gateway. response: %+v", r.RawResponse)
					}
				}
				return nil
			})),
	}
}
