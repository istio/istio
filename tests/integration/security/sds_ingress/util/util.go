//go:build integ
// +build integ

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

package util

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	// The ID/name for the certificate chain in kubernetes tls secret.
	tlsScrtCert = "tls.crt"
	// The ID/name for the k8sKey in kubernetes tls secret.
	tlsScrtKey = "tls.key"
	// The ID/name for the CA certificate in kubernetes tls secret
	tlsScrtCaCert = "ca.crt"
	// The ID/name for the CRL in kubernetes tls secret
	tlsScrtCaCrl = "ca.crl"
	// The ID/name for the certificate chain in kubernetes generic secret.
	genericScrtCert = "cert"
	// The ID/name for the private key in kubernetes generic secret.
	genericScrtKey = "key"
	// The ID/name for the CA certificate in kubernetes generic secret.
	genericScrtCaCert = "cacert"
	// The ID/name for the CRL in kubernetes generic secret.
	genericScrtCaCrl = "crl"
	ASvc             = "a"
	VMSvc            = "vm"
)

type EchoDeployments struct {
	ServerNs namespace.Instance
	All      echo.Instances
}

type IngressCredential struct {
	PrivateKey  string
	Certificate string
	CaCert      string
	Crl         string
}

var IngressCredentialA = IngressCredential{
	PrivateKey:  TLSServerKeyA,
	Certificate: TLSServerCertA,
	CaCert:      CaCertA,
}

var IngressCredentialServerKeyCertA = IngressCredential{
	PrivateKey:  TLSServerKeyA,
	Certificate: TLSServerCertA,
}

var IngressCredentialCaCertA = IngressCredential{
	CaCert: CaCertA,
}

var IngressCredentialB = IngressCredential{
	PrivateKey:  TLSServerKeyB,
	Certificate: TLSServerCertB,
	CaCert:      CaCertB,
}

var IngressCredentialServerKeyCertB = IngressCredential{
	PrivateKey:  TLSServerKeyB,
	Certificate: TLSServerCertB,
}

var (
	A  echo.Instances
	VM echo.Instances
)

// IngressKubeSecretYAML will generate a credential for a gateway
func IngressKubeSecretYAML(name, namespace string, ingressType CallType, ingressCred IngressCredential) string {
	// Create Kubernetes secret for ingress gateway
	secret := createSecret(ingressType, name, namespace, ingressCred, true)
	by, err := yaml.Marshal(secret)
	if err != nil {
		panic(err)
	}
	return string(by) + "\n---\n"
}

// CreateIngressKubeSecret reads credential names from credNames and key/cert from ingressCred,
// and creates K8s secrets for ingress gateway.
// nolint: interfacer
func CreateIngressKubeSecret(t framework.TestContext, credName string,
	ingressType CallType, ingressCred IngressCredential, isCompoundAndNotGeneric bool, clusters ...cluster.Cluster,
) {
	t.Helper()

	// Get namespace for ingress gateway pod.
	istioCfg := istio.DefaultConfigOrFail(t, t)
	systemNS := namespace.ClaimOrFail(t, istioCfg.SystemNamespace)
	CreateIngressKubeSecretInNamespace(t, credName, ingressType, ingressCred, isCompoundAndNotGeneric, systemNS.Name(), clusters...)
}

// CreateIngressKubeSecretInNamespace  reads credential names from credNames and key/cert from ingressCred,
// and creates K8s secrets for ingress gateway in the given namespace.
func CreateIngressKubeSecretInNamespace(t framework.TestContext, credName string,
	ingressType CallType, ingressCred IngressCredential, isCompoundAndNotGeneric bool, ns string, clusters ...cluster.Cluster,
) {
	t.Helper()

	t.CleanupConditionally(func() {
		deleteKubeSecret(t, credName)
	})

	// Create Kubernetes secret for ingress gateway
	wg := multierror.Group{}
	if len(clusters) == 0 {
		clusters = t.Clusters()
	}
	for _, c := range clusters {
		wg.Go(func() error {
			secret := createSecret(ingressType, credName, ns, ingressCred, isCompoundAndNotGeneric)
			_, err := c.Kube().CoreV1().Secrets(ns).Create(context.TODO(), secret, metav1.CreateOptions{})
			if err != nil {
				if errors.IsAlreadyExists(err) {
					if _, err := c.Kube().CoreV1().Secrets(ns).Update(context.TODO(), secret, metav1.UpdateOptions{}); err != nil {
						return fmt.Errorf("failed to update secret (error: %s)", err)
					}
				} else {
					return fmt.Errorf("failed to update secret (error: %s)", err)
				}
			}
			// Check if Kubernetes secret is ready
			return retry.UntilSuccess(func() error {
				_, err := c.Kube().CoreV1().Secrets(ns).Get(context.TODO(), credName, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("secret %v not found: %v", credName, err)
				}
				return nil
			}, retry.Timeout(time.Second*5))
		})
	}
	if err := wg.Wait().ErrorOrNil(); err != nil {
		t.Fatal(err)
	}
}

// deleteKubeSecret deletes a secret
// nolint: interfacer
func deleteKubeSecret(t framework.TestContext, credName string) {
	// Get namespace for ingress gateway pod.
	istioCfg := istio.DefaultConfigOrFail(t, t)
	systemNS := namespace.ClaimOrFail(t, istioCfg.SystemNamespace)

	// Delete Kubernetes secret for ingress gateway
	c := t.Clusters().Default()
	var immediate int64
	err := c.Kube().CoreV1().Secrets(systemNS.Name()).Delete(context.TODO(), credName,
		metav1.DeleteOptions{GracePeriodSeconds: &immediate})
	if err != nil && !errors.IsNotFound(err) {
		t.Fatalf("Failed to delete secret (error: %s)", err)
	}
}

// createSecret creates a kubernetes secret which stores private key, server certificate for TLS ingress gateway.
// For mTLS ingress gateway, createSecret adds ca certificate into the secret object.

func createSecret(ingressType CallType, cn, ns string, ic IngressCredential, isCompoundAndNotGeneric bool) *v1.Secret {
	if ingressType == Mtls {
		if isCompoundAndNotGeneric {
			return &v1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      cn,
					Namespace: ns,
				},
				Data: map[string][]byte{
					tlsScrtCert:   []byte(ic.Certificate),
					tlsScrtKey:    []byte(ic.PrivateKey),
					tlsScrtCaCert: []byte(ic.CaCert),
					tlsScrtCaCrl:  []byte(ic.Crl),
				},
			}
		}
		return &v1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      cn,
				Namespace: ns,
			},
			Data: map[string][]byte{
				genericScrtCert:   []byte(ic.Certificate),
				genericScrtKey:    []byte(ic.PrivateKey),
				genericScrtCaCert: []byte(ic.CaCert),
				genericScrtCaCrl:  []byte(ic.Crl),
			},
		}
	}
	data := map[string][]byte{}
	if ic.Certificate != "" {
		data[tlsScrtCert] = []byte(ic.Certificate)
	}
	if ic.PrivateKey != "" {
		data[tlsScrtKey] = []byte(ic.PrivateKey)
	}
	if ic.CaCert != "" {
		data[tlsScrtCaCert] = []byte(ic.CaCert)
	}
	return &v1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cn,
			Namespace: ns,
		},
		Data: data,
	}
}

// CallType defines ingress gateway type
type CallType int

const (
	TLS CallType = iota
	Mtls
)

type ExpectedResponse struct {
	StatusCode                   int
	SkipErrorMessageVerification bool
	ErrorMessage                 string
}

type TLSContext struct {
	// CaCert is inline base64 encoded root certificate that authenticates server certificate provided
	// by ingress gateway.
	CaCert string
	// PrivateKey is inline base64 encoded private key for test client.
	PrivateKey string
	// Cert is inline base64 encoded certificate for test client.
	Cert string
}

// SendRequestOrFail makes HTTPS request to ingress gateway to visit product page
func SendRequestOrFail(ctx framework.TestContext, ing ingress.Instance, host string, path string,
	callType CallType, tlsCtx TLSContext, exRsp ExpectedResponse,
) {
	doSendRequestsOrFail(ctx, ing, host, path, callType, tlsCtx, exRsp, false /* useHTTP3 */)
}

func SendQUICRequestsOrFail(ctx framework.TestContext, ing ingress.Instance, host string, path string,
	callType CallType, tlsCtx TLSContext, exRsp ExpectedResponse,
) {
	doSendRequestsOrFail(ctx, ing, host, path, callType, tlsCtx, exRsp, true /* useHTTP3 */)
}

func doSendRequestsOrFail(ctx framework.TestContext, ing ingress.Instance, host string, path string,
	callType CallType, tlsCtx TLSContext, exRsp ExpectedResponse, useHTTP3 bool,
) {
	ctx.Helper()
	opts := echo.CallOptions{
		Timeout: time.Second,
		Retry: echo.Retry{
			Options: []retry.Option{retry.Timeout(time.Minute * 2)},
		},
		Port: echo.Port{
			Protocol: protocol.HTTPS,
		},
		HTTP: echo.HTTP{
			HTTP3:   useHTTP3,
			Path:    fmt.Sprintf("/%s", path),
			Headers: headers.New().WithHost(host).Build(),
		},
		TLS: echo.TLS{
			CaCert: tlsCtx.CaCert,
		},
		Check: func(result echo.CallResult, err error) error {
			// Check that the error message is expected.
			if err != nil {
				// If expected error message is empty, but we got some error
				// message then it should be treated as error when error message
				// verification is not skipped. Error message verification is skipped
				// when the error message is non-deterministic.
				if !exRsp.SkipErrorMessageVerification && len(exRsp.ErrorMessage) == 0 {
					return fmt.Errorf("unexpected error: %w", err)
				}
				if !exRsp.SkipErrorMessageVerification && !strings.Contains(err.Error(), exRsp.ErrorMessage) {
					return fmt.Errorf("expected response error message %s but got %w",
						exRsp.ErrorMessage, err)
				}
				return nil
			}
			if callType == Mtls {
				return check.And(check.Status(exRsp.StatusCode), check.MTLSForHTTP()).Check(result, nil)
			}

			return check.Status(exRsp.StatusCode).Check(result, nil)
		},
	}

	if callType == Mtls {
		opts.TLS.Key = tlsCtx.PrivateKey
		opts.TLS.Cert = tlsCtx.Cert
	}

	// Certs occasionally take quite a while to become active in Envoy, so retry for a long time (2min)
	ing.CallOrFail(ctx, opts)
}

// RotateSecrets deletes kubernetes secrets by name in credNames and creates same secrets using key/cert
// from ingressCred.
func RotateSecrets(ctx framework.TestContext, credName string, // nolint:interfacer
	ingressType CallType, ingressCred IngressCredential, isCompoundAndNotGeneric bool,
) {
	ctx.Helper()
	c := ctx.Clusters().Default()
	ist := istio.GetOrFail(ctx)
	systemNS := namespace.ClaimOrFail(ctx, ist.Settings().SystemNamespace)
	scrt, err := c.Kube().CoreV1().Secrets(systemNS.Name()).Get(context.TODO(), credName, metav1.GetOptions{})
	if err != nil {
		ctx.Errorf("Failed to get secret %s:%s (error: %s)", systemNS.Name(), credName, err)
	}
	scrt = updateSecret(ingressType, scrt, ingressCred, isCompoundAndNotGeneric)
	if _, err = c.Kube().CoreV1().Secrets(systemNS.Name()).Update(context.TODO(), scrt, metav1.UpdateOptions{}); err != nil {
		ctx.Errorf("Failed to update secret %s:%s (error: %s)", scrt.Namespace, scrt.Name, err)
	}
	// Check if Kubernetes secret is ready
	retry.UntilSuccessOrFail(ctx, func() error {
		_, err := c.Kube().CoreV1().Secrets(systemNS.Name()).Get(context.TODO(), credName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("secret %v not found: %v", credName, err)
		}
		return nil
	}, retry.Timeout(time.Second*5))
}

// createSecret creates a kubernetes secret which stores private key, server certificate for TLS ingress gateway.
// For mTLS ingress gateway, createSecret adds ca certificate into the secret object.
func updateSecret(ingressType CallType, scrt *v1.Secret, ic IngressCredential, isCompoundAndNotGeneric bool) *v1.Secret {
	if ingressType == Mtls {
		if isCompoundAndNotGeneric {
			scrt.Data[tlsScrtCert] = []byte(ic.Certificate)
			scrt.Data[tlsScrtKey] = []byte(ic.PrivateKey)
			scrt.Data[tlsScrtCaCert] = []byte(ic.CaCert)
			scrt.Data[tlsScrtCaCrl] = []byte(ic.Crl)
		} else {
			scrt.Data[genericScrtCert] = []byte(ic.Certificate)
			scrt.Data[genericScrtKey] = []byte(ic.PrivateKey)
			scrt.Data[genericScrtCaCert] = []byte(ic.CaCert)
			scrt.Data[genericScrtCaCrl] = []byte(ic.Crl)
		}
	} else {
		scrt.Data[tlsScrtCert] = []byte(ic.Certificate)
		scrt.Data[tlsScrtKey] = []byte(ic.PrivateKey)
	}
	return scrt
}

func EchoConfig(service string, ns namespace.Instance, buildVM bool) echo.Config {
	return echo.Config{
		Service:   service,
		Namespace: ns,
		Ports: []echo.Port{
			{
				Name:     "http",
				Protocol: protocol.HTTP,
				// We use a port > 1024 to not require root
				WorkloadPort: 8090,
			},
		},
		DeployAsVM: buildVM,
	}
}

func SetupTest(ctx resource.Context, customCfg *[]echo.Config, customNs namespace.Getter) error {
	var customConfig []echo.Config
	buildVM := !ctx.Settings().Skip(echo.VM)
	a := EchoConfig(ASvc, customNs.Get(), false)
	vm := EchoConfig(VMSvc, customNs.Get(), buildVM)
	customConfig = append(customConfig, a, vm)
	*customCfg = customConfig
	return nil
}

type TestConfig struct {
	Mode           string
	CredentialName string
	Host           string
	ServiceName    string
	GatewayLabel   string
}

const vsTemplate = `
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: {{.CredentialName}}
spec:
  hosts:
  - "{{.Host}}"
  gateways:
  - {{.CredentialName}}
  http:
  - match:
    - uri:
        exact: /{{.CredentialName}}
    route:
    - destination:
        host: {{.ServiceName}}
        port:
          number: 80
`

const gwTemplate = `
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: {{.CredentialName}}
spec:
  selector:
    istio: {{.GatewayLabel | default "ingressgateway"}}
  servers:
  - port:
      number: 443
      name: https
      protocol: HTTPS
    tls:
      mode: {{.Mode}}
      credentialName: "{{.CredentialName}}"
    hosts:
    - "{{.Host}}"
`

func SetupConfig(ctx framework.TestContext, ns namespace.Instance, config ...TestConfig) {
	b := ctx.ConfigIstio().New()
	for _, c := range config {
		b.Eval(ns.Name(), c, vsTemplate)
		b.Eval(ns.Name(), c, gwTemplate)
	}
	b.ApplyOrFail(ctx)
}

// RunTestMultiMtlsGateways deploys multiple mTLS gateways with SDS enabled, and creates kubernetes secret that stores
// private key, server certificate and CA certificate for each mTLS gateway. Verifies that all gateways are able to terminate
// mTLS connections successfully.
func RunTestMultiMtlsGateways(ctx framework.TestContext, inst istio.Instance, ns namespace.Getter) { // nolint:interfacer
	var credNames []string
	var tests []TestConfig
	echotest.New(ctx, A).
		SetupForDestination(func(ctx framework.TestContext, to echo.Target) error {
			for i := 1; i < 6; i++ {
				cred := fmt.Sprintf("runtestmultimtlsgateways-%d", i)
				tests = append(tests, TestConfig{
					Mode:           "MUTUAL",
					CredentialName: cred,
					Host:           fmt.Sprintf("runtestmultimtlsgateways%d.example.com", i),
					ServiceName:    to.Config().Service,
					GatewayLabel:   inst.Settings().IngressGatewayIstioLabel,
				})
				credNames = append(credNames, cred)
			}
			SetupConfig(ctx, ns.Get(), tests...)
			return nil
		}).
		To(echotest.SimplePodServiceAndAllSpecial(1)).
		RunFromClusters(func(ctx framework.TestContext, fromCluster cluster.Cluster, to echo.Target) {
			for _, cn := range credNames {
				CreateIngressKubeSecret(ctx, cn, Mtls, IngressCredentialA, false)
			}

			ing := inst.IngressFor(fromCluster)
			if ing == nil {
				ctx.Skip()
			}
			tlsContext := TLSContext{
				CaCert:     CaCertA,
				PrivateKey: TLSClientKeyA,
				Cert:       TLSClientCertA,
			}
			callType := Mtls

			for _, h := range tests {
				ctx.NewSubTest(h.Host).Run(func(t framework.TestContext) {
					SendRequestOrFail(t, ing, h.Host, h.CredentialName, callType, tlsContext,
						ExpectedResponse{StatusCode: http.StatusOK})
				})
			}
		})
}

// RunTestMultiTLSGateways deploys multiple TLS gateways with SDS enabled, and creates kubernetes secret that stores
// private key and server certificate for each TLS gateway. Verifies that all gateways are able to terminate
// SSL connections successfully.
func RunTestMultiTLSGateways(t framework.TestContext, inst istio.Instance, ns namespace.Getter) { // nolint:interfacer
	var credNames []string
	var tests []TestConfig
	echotest.New(t, A).
		SetupForDestination(func(t framework.TestContext, to echo.Target) error {
			for i := 1; i < 6; i++ {
				cred := fmt.Sprintf("runtestmultitlsgateways-%d", i)
				tests = append(tests, TestConfig{
					Mode:           "SIMPLE",
					CredentialName: cred,
					Host:           fmt.Sprintf("runtestmultitlsgateways%d.example.com", i),
					ServiceName:    to.Config().Service,
					GatewayLabel:   inst.Settings().IngressGatewayIstioLabel,
				})
				credNames = append(credNames, cred)
			}
			SetupConfig(t, ns.Get(), tests...)
			return nil
		}).
		To(echotest.SimplePodServiceAndAllSpecial(1)).
		RunFromClusters(func(t framework.TestContext, fromCluster cluster.Cluster, to echo.Target) {
			for _, cn := range credNames {
				CreateIngressKubeSecret(t, cn, TLS, IngressCredentialA, false)
			}

			ing := inst.IngressFor(fromCluster)
			if ing == nil {
				t.Skip()
			}
			tlsContext := TLSContext{
				CaCert: CaCertA,
			}
			callType := TLS

			for _, h := range tests {
				t.NewSubTest(h.Host).Run(func(t framework.TestContext) {
					SendRequestOrFail(t, ing, h.Host, h.CredentialName, callType, tlsContext,
						ExpectedResponse{StatusCode: http.StatusOK})
				})
			}
		})
}

// RunTestMultiQUICGateways deploys multiple TLS/mTLS gateways with SDS enabled, and creates kubernetes secret that stores
// private key and server certificate for each TLS/mTLS gateway. Verifies that all gateways are able to terminate
// QUIC connections successfully.
func RunTestMultiQUICGateways(t framework.TestContext, inst istio.Instance, callType CallType, ns namespace.Getter) {
	var credNames []string
	var tests []TestConfig
	allInstances := []echo.Instances{A, VM}
	for _, instances := range allInstances {
		echotest.New(t, instances).
			SetupForDestination(func(t framework.TestContext, to echo.Target) error {
				for i := 1; i < 6; i++ {
					cred := fmt.Sprintf("runtestmultitlsgateways-%d", i)
					mode := "SIMPLE"
					if callType == Mtls {
						mode = "MUTUAL"
					}
					tests = append(tests, TestConfig{
						Mode:           mode,
						CredentialName: cred,
						Host:           fmt.Sprintf("runtestmultitlsgateways%d.example.com", i),
						ServiceName:    to.Config().Service,
						GatewayLabel:   inst.Settings().IngressGatewayIstioLabel,
					})
					credNames = append(credNames, cred)
				}
				SetupConfig(t, ns.Get(), tests...)
				return nil
			}).
			To(echotest.SimplePodServiceAndAllSpecial(1)).
			RunFromClusters(func(t framework.TestContext, fromCluster cluster.Cluster, to echo.Target) {
				for _, cn := range credNames {
					CreateIngressKubeSecret(t, cn, TLS, IngressCredentialA, false)
				}

				ing := inst.IngressFor(fromCluster)
				if ing == nil {
					t.Skip()
				}
				tlsContext := TLSContext{
					CaCert: CaCertA,
				}
				if callType == Mtls {
					tlsContext = TLSContext{
						CaCert:     CaCertA,
						PrivateKey: TLSClientKeyA,
						Cert:       TLSClientCertA,
					}
				}

				for _, h := range tests {
					t.NewSubTest(h.Host).Run(func(t framework.TestContext) {
						SendQUICRequestsOrFail(t, ing, h.Host, h.CredentialName, callType, tlsContext,
							ExpectedResponse{StatusCode: http.StatusOK})
					})
				}
			})
	}
}

func SetInstances(apps echo.Services) error {
	for index, namespacedName := range apps.NamespacedNames() {
		switch {
		case namespacedName.Name == "a":
			A = apps[index]
		case namespacedName.Name == "vm":
			VM = apps[index]
		}
	}
	return nil
}

// Some cloud platform may throw the following error during creation of the service with mixed TCP/UDP protocols:
// "Error syncing load balancer: failed to ensure load balancer: mixed protocol is not supported for LoadBalancer".
// Make sure the service is up and running before proceeding with the test.
func WaitForIngressQUICService(t framework.TestContext, ns string) error {
	_, err := retry.UntilComplete(func() (any, bool, error) {
		services, err := t.Clusters().Default().Kube().CoreV1().Services(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return nil, false, err
		}
		if len(services.Items) == 0 {
			return nil, false, fmt.Errorf("still waiting for the service in namespace %s to be created", ns)
		}

		// Fetch events to check for the service status
		fieldSelector := fmt.Sprintf("involvedObject.kind=Service,involvedObject.name=%s", "istio-ingressgateway")
		events, err := t.Clusters().Default().Kube().CoreV1().Events(ns).List(context.TODO(), metav1.ListOptions{
			FieldSelector: fieldSelector,
		})
		if err != nil {
			return nil, false, err
		}

		// Verify that "instio-ingressgateway" service is not stuck with creation error
		for _, ev := range events.Items {
			if strings.Contains(ev.Message, "mixed protocol is not supported for LoadBalancer") {
				return nil, true, fmt.Errorf("the QUIC mixed service is not supported")
			}
		}
		return nil, true, nil
	}, retry.Delay(1*time.Second), retry.Timeout(30*time.Second), retry.Converge(0))

	return err
}
