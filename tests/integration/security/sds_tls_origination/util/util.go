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
	"context"
	"fmt"
	"html/template"
	"io/ioutil"
	"path"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"

	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/resource"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)

func MustReadCert(t *testing.T, f string) string {
	b, err := ioutil.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs/dns", f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}

type TLSCredential struct {
	PrivateKey string
	ClientCert string
	CaCert     string
}

const (
	// The ID/name for the certificate chain in kubernetes tls secret.
	tlsScrtCert = "tls.crt"
	// The ID/name for the k8sKey in kubernetes tls secret.
	tlsScrtKey = "tls.key"
	// The ID/name for the CA certificate in kubernetes tls secret
	tlsScrtCaCert = "ca.crt"
	// The ID/name for the certificate chain in kubernetes generic secret.
	genericScrtCert = "cert"
	// The ID/name for the private key in kubernetes generic secret.
	genericScrtKey = "key"
	// The ID/name for the CA certificate in kubernetes generic secret.
	genericScrtCaCert = "cacert"
)

// CreateKubeSecret reads credential names from credNames and key/cert from TLSCredential,
// and creates K8s secrets for gateway.
// nolint: interfacer
func CreateKubeSecret(t test.Failer, ctx framework.TestContext, credNames []string,
	credentialType string, egressCred TLSCredential, isNotGeneric bool) {
	t.Helper()
	// Get namespace for gateway pod.
	istioCfg := istio.DefaultConfigOrFail(t, ctx)
	systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)

	if len(credNames) == 0 {
		ctx.Log("no credential names are specified, skip creating secret")
		return
	}
	// Create Kubernetes secret for gateway
	cluster := ctx.Environment().(*kube.Environment).KubeClusters[0]
	for _, cn := range credNames {
		secret := createSecret(credentialType, cn, systemNS.Name(), egressCred, isNotGeneric)
		_, err := cluster.CoreV1().Secrets(systemNS.Name()).Create(context.TODO(), secret, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create secret (error: %s)", err)
		}
	}
	// Check if Kubernetes secret is ready
	retry.UntilSuccessOrFail(t, func() error {
		for _, cn := range credNames {
			_, err := cluster.CoreV1().Secrets(systemNS.Name()).Get(context.TODO(), cn, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("secret %v not found: %v", cn, err)
			}
		}
		return nil
	}, retry.Timeout(time.Second*5))
}

// createSecret creates a kubernetes secret which stores CA cert for SIMPLE TLS.
// For mTLS gateway, createSecret adds client cert and key into the secret object.
func createSecret(credentialType string, cn, ns string, ic TLSCredential, isNotGeneric bool) *v1.Secret {
	if credentialType == "MUTUAL" {
		if isNotGeneric {
			return &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cn,
					Namespace: ns,
				},
				Data: map[string][]byte{
					tlsScrtCert:   []byte(ic.ClientCert),
					tlsScrtKey:    []byte(ic.PrivateKey),
					tlsScrtCaCert: []byte(ic.CaCert),
				},
			}
		}
		return &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cn,
				Namespace: ns,
			},
			Data: map[string][]byte{
				genericScrtCert:   []byte(ic.ClientCert),
				genericScrtKey:    []byte(ic.PrivateKey),
				genericScrtCaCert: []byte(ic.CaCert),
			},
		}
	}
	if isNotGeneric {
		return &v1.Secret{}
	}
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cn,
			Namespace: ns,
		},
		Data: map[string][]byte{
			genericScrtCaCert: []byte(ic.CaCert),
		},
	}
}

// SetupEcho creates two namespaces client and server. It also brings up two echo instances server and
// client in respective namespaces. HTTP and HTTPS port on the server echo are set up. Egress Gateway is set up in the
// service namespace to handle egress for "external" calls.
func SetupEcho(t *testing.T, ctx resource.Context) (echo.Instance, echo.Instance, namespace.Instance, namespace.Instance) {
	clientNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "client",
		Inject: true,
	})
	serverNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "server",
		Inject: true,
	})

	var internalClient, externalServer echo.Instance
	echoboot.NewBuilderOrFail(t, ctx).
		With(&internalClient, echo.Config{
			Service:   "client",
			Namespace: clientNamespace,
			Ports:     []echo.Port{},
			Subsets: []echo.SubsetConfig{{
				Version: "v1",
			}},
		}).
		With(&externalServer, echo.Config{
			Service:   "server",
			Namespace: serverNamespace,
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

	// Apply Egress Gateway for service namespace to originate external traffic
	createGateway(t, ctx, clientNamespace, serverNamespace)

	if err := WaitUntilNotCallable(internalClient, externalServer); err != nil {
		t.Fatalf("failed to apply sidecar, %v", err)
	}

	return internalClient, externalServer, clientNamespace, serverNamespace
}

const (
	Gateway = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: istio-egressgateway
spec:
  selector:
    istio: egressgateway
  servers:
    - port:
        number: 80
        name: http-port-for-tls-origination
        protocol: HTTP
      hosts:
        - server.{{.ServerNamespace}}.svc.cluster.local
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: egressgateway-for-server
spec:
  host: istio-egressgateway.istio-system.svc.cluster.local
  subsets:
  - name: server
`
	VirtualService = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route-via-egressgateway
spec:
  hosts:
    - server.{{.ServerNamespace}}.svc.cluster.local
  gateways:
    - istio-egressgateway
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
              number: 80
          weight: 100
    - match:
        - gateways:
            - istio-egressgateway
          port: 80
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
func createGateway(t *testing.T, ctx resource.Context, clientNamespace namespace.Instance, serverNamespace namespace.Instance) {
	tmplGateway, err := template.New("Gateway").Parse(Gateway)
	if err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	var bufGateway bytes.Buffer
	if err := tmplGateway.Execute(&bufGateway, map[string]string{"ServerNamespace": serverNamespace.Name()}); err != nil {
		t.Fatalf("failed to create template: %v", err)
	}
	if err := ctx.Config().ApplyYAML(clientNamespace.Name(), bufGateway.String()); err != nil {
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
	if err := ctx.Config().ApplyYAML(clientNamespace.Name(), bufVS.String()); err != nil {
		t.Fatalf("failed to apply gateway: %v. template: %v", err, bufVS.String())
	}
}

const (
	// Destination Rule configs
	DestinationRuleConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: originate-tls-for-server
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
func CreateDestinationRule(t *testing.T, serverNamespace namespace.Instance,
	destinationRuleMode string, credentialName string) bytes.Buffer {

	destinationRuleToParse := DestinationRuleConfig

	tmpl, err := template.New("DestinationRule").Parse(destinationRuleToParse)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"ServerNamespace": serverNamespace.Name(),
		"Mode": destinationRuleMode, "CredentialName": credentialName}); err != nil {
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
