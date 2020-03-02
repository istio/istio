//  Copyright 2019 Istio Authors
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
	"fmt"
	"path"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/util/retry"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

// CallType defines type of bookinfo gateway
type GatewayType int

const (
	// The ID/name for the certificate chain in kubernetes tls secret.
	tlsScrtCert = "tls.crt"
	// The ID/name for the k8sKey in kubernetes tls secret.
	tlsScrtKey = "tls.key"
	// The ID/name for the certificate chain in kubernetes generic secret.
	genericScrtCert = "cert"
	// The ID/name for the private key in kubernetes generic secret.
	genericScrtKey = "key"
	// The ID/name for the CA certificate in kubernetes generic secret.
	genericScrtCaCert = "cacert"

	SingleTLSGateway  GatewayType = 0
	SingleMTLSGateway GatewayType = 1
	MultiTLSGateway   GatewayType = 2
	MultiMTLSGateway  GatewayType = 3
)

type IngressCredential struct {
	PrivateKey string
	ServerCert string
	CaCert     string
}

var IngressCredentialA = IngressCredential{
	PrivateKey: TLSServerKeyA,
	ServerCert: TLSServerCertA,
	CaCert:     CaCertA,
}
var IngressCredentialServerKeyCertA = IngressCredential{
	PrivateKey: TLSServerKeyA,
	ServerCert: TLSServerCertA,
}
var IngressCredentialCaCertA = IngressCredential{
	CaCert: CaCertA,
}
var IngressCredentialB = IngressCredential{
	PrivateKey: TLSServerKeyB,
	ServerCert: TLSServerCertB,
	CaCert:     CaCertB,
}
var IngressCredentialServerKeyCertB = IngressCredential{
	PrivateKey: TLSServerKeyB,
	ServerCert: TLSServerCertB,
}
var IngressCredentialCaCertB = IngressCredential{
	CaCert: CaCertB,
}

var (
	credNames = []string{"bookinfo-credential-1", "bookinfo-credential-2", "bookinfo-credential-3",
		"bookinfo-credential-4", "bookinfo-credential-5"}
	hosts = []string{"bookinfo1.example.com", "bookinfo2.example.com", "bookinfo3.example.com",
		"bookinfo4.example.com", "bookinfo5.example.com"}
)

// CreateIngressKubeSecret reads credential names from credNames and key/cert from ingressCred,
// and creates K8s secrets for ingress gateway.
// nolint: interfacer
func CreateIngressKubeSecret(t test.Failer, ctx framework.TestContext, credNames []string,
	ingressType ingress.CallType, ingressCred IngressCredential) {
	t.Helper()
	// Get namespace for ingress gateway pod.
	istioCfg := istio.DefaultConfigOrFail(t, ctx)
	systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)

	if len(credNames) == 0 {
		ctx.Log("no credential names are specified, skip creating ingress secret")
		return
	}
	// Create Kubernetes secret for ingress gateway
	kubeAccessor := ctx.Environment().(*kube.Environment).Accessor
	for _, cn := range credNames {
		secret := createSecret(ingressType, cn, systemNS.Name(), ingressCred)
		err := kubeAccessor.CreateSecret(systemNS.Name(), secret)
		if err != nil {
			t.Fatalf("Failed to create secret (error: %s)", err)
		}
	}
	// Check if Kubernetes secret is ready
	retry.UntilSuccessOrFail(t, func() error {
		for _, cn := range credNames {
			_, err := kubeAccessor.GetSecret(systemNS.Name()).Get(cn, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("secret %v not found: %v", cn, err)
			}
		}
		return nil
	}, retry.Timeout(time.Second*5))
}

// DeleteIngressKubeSecret deletes a secret
// nolint: interfacer
func DeleteIngressKubeSecret(t test.Failer, ctx framework.TestContext, credNames []string) {
	// Get namespace for ingress gateway pod.
	istioCfg := istio.DefaultConfigOrFail(t, ctx)
	systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)

	if len(credNames) == 0 {
		ctx.Log("no credential names are specified, skip creating ingress secret")
		return
	}
	// Create Kubernetes secret for ingress gateway
	kubeAccessor := ctx.Environment().(*kube.Environment).Accessor
	for _, cn := range credNames {
		err := kubeAccessor.DeleteSecret(systemNS.Name(), cn)
		if err != nil {
			t.Fatalf("Failed to create secret (error: %s)", err)
		}
	}
}

// createSecret creates a kubernetes secret which stores private key, server certificate for TLS ingress gateway.
// For mTLS ingress gateway, createSecret adds ca certificate into the secret object.
func createSecret(ingressType ingress.CallType, cn, ns string, ic IngressCredential) *v1.Secret {
	if ingressType == ingress.Mtls {
		return &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cn,
				Namespace: ns,
			},
			Data: map[string][]byte{
				genericScrtCert:   []byte(ic.ServerCert),
				genericScrtKey:    []byte(ic.PrivateKey),
				genericScrtCaCert: []byte(ic.CaCert),
			},
		}
	}
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cn,
			Namespace: ns,
		},
		Data: map[string][]byte{
			tlsScrtCert: []byte(ic.ServerCert),
			tlsScrtKey:  []byte(ic.PrivateKey),
		},
	}
}

type ExpectedResponse struct {
	ResponseCode int
	ErrorMessage string
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

// VisitProductPage makes HTTPS request to ingress gateway to visit product page
func VisitProductPage(ing ingress.Instance, host string, callType ingress.CallType, tlsCtx TLSContext,
	timeout time.Duration, exRsp ExpectedResponse, t *testing.T) error {
	t.Helper()
	endpointAddress := ing.HTTPSAddress()
	return retry.UntilSuccess(func() error {
		response, err := ing.Call(ingress.CallOptions{
			Host:       host,
			Path:       "/backend",
			CaCert:     tlsCtx.CaCert,
			PrivateKey: tlsCtx.PrivateKey,
			Cert:       tlsCtx.Cert,
			CallType:   callType,
			Address:    endpointAddress,
		})
		errorMatch := true
		if err != nil {
			if !strings.Contains(err.Error(), exRsp.ErrorMessage) {
				errorMatch = false
			}
		}

		status := response.Code
		if status == exRsp.ResponseCode && errorMatch {
			return nil
		} else if status != exRsp.ResponseCode {
			return fmt.Errorf("expected response code %d but got %d", exRsp.ResponseCode, status)
		} else {
			return fmt.Errorf("expected response error message %s but got %s", exRsp.ErrorMessage, err.Error())
		}

		return nil
	}, retry.Delay(time.Second))
}

// RotateSecrets deletes kubernetes secrets by name in credNames and creates same secrets using key/cert
// from ingressCred.
func RotateSecrets(t *testing.T, ctx framework.TestContext, credNames []string, // nolint:interfacer
	ingressType ingress.CallType, ingressCred IngressCredential) {
	t.Helper()
	istioCfg := istio.DefaultConfigOrFail(t, ctx)
	systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)
	kubeAccessor := ctx.Environment().(*kube.Environment).Accessor
	for _, cn := range credNames {
		scrt, err := kubeAccessor.GetSecret(systemNS.Name()).Get(cn, metav1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to get secret %s:%s (error: %s)", scrt.Namespace, scrt.Name, err)
			continue
		}
		scrt = updateSecret(ingressType, scrt, ingressCred)
		if _, err = kubeAccessor.GetSecret(systemNS.Name()).Update(scrt); err != nil {
			t.Errorf("Failed to update secret %s:%s (error: %s)", scrt.Namespace, scrt.Name, err)
		}
	}
	// Check if Kubernetes secret is ready
	retry.UntilSuccessOrFail(t, func() error {
		for _, cn := range credNames {
			_, err := kubeAccessor.GetSecret(systemNS.Name()).Get(cn, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("secret %v not found: %v", cn, err)
			}
		}
		return nil
	}, retry.Timeout(time.Second*5))
}

// createSecret creates a kubernetes secret which stores private key, server certificate for TLS ingress gateway.
// For mTLS ingress gateway, createSecret adds ca certificate into the secret object.
func updateSecret(ingressType ingress.CallType, scrt *v1.Secret, ic IngressCredential) *v1.Secret {
	if ingressType == ingress.Mtls {
		scrt.Data[genericScrtCert] = []byte(ic.ServerCert)
		scrt.Data[genericScrtKey] = []byte(ic.PrivateKey)
		scrt.Data[genericScrtCaCert] = []byte(ic.CaCert)

	} else {
		scrt.Data[tlsScrtCert] = []byte(ic.ServerCert)
		scrt.Data[tlsScrtKey] = []byte(ic.PrivateKey)
	}
	return scrt
}

// DeployBookinfo deploys bookinfo application, and deploys gateway with various type.
// nolint: interfacer
func DeployBookinfo(t *testing.T, ctx framework.TestContext, g galley.Instance, gatewayType GatewayType) {
	serverNs, err := namespace.New(ctx, namespace.Config{
		Prefix: "ingress",
		Inject: true,
	})
	if err != nil {
		t.Fatalf("Could not create istio-bookinfo Namespace; err:%v", err)
	}
	var a echo.Instance
	echoboot.NewBuilderOrFail(ctx, ctx).
		With(&a, echo.Config{
			Service:   "server",
			Namespace: serverNs,
			Ports: []echo.Port{
				{
					Name:     "http",
					Protocol: protocol.HTTP,
					// We use a port > 1024 to not require root
					InstancePort: 8090,
				},
			},
			Galley: g,
		}).
		BuildOrFail(ctx)

	// Backup the original bookinfo root.
	originBookInfoRoot := env.BookInfoRoot
	env.BookInfoRoot = path.Join(env.IstioSrc, "tests/integration/security/sds_ingress/")
	var gatewayPath, virtualSvcPath, destRulePath bookinfo.ConfigFile
	switch gatewayType {
	case SingleTLSGateway:
		gatewayPath = "testdata/bookinfo-single-tls-gateway.yaml"
		virtualSvcPath = "testdata/bookinfo-single-virtualservice.yaml"
		destRulePath = "testdata/bookinfo-productpage-destinationrule.yaml"
	case SingleMTLSGateway:
		gatewayPath = "testdata/bookinfo-single-mtls-gateway.yaml"
		virtualSvcPath = "testdata/bookinfo-single-virtualservice.yaml"
		destRulePath = "testdata/bookinfo-productpage-destinationrule.yaml"
	case MultiTLSGateway:
		gatewayPath = "testdata/bookinfo-multiple-tls-gateways.yaml"
		virtualSvcPath = "testdata/bookinfo-multiple-virtualservices.yaml"
		destRulePath = "testdata/bookinfo-productpage-destinationrule.yaml"
	case MultiMTLSGateway:
		gatewayPath = "testdata/bookinfo-multiple-mtls-gateways.yaml"
		virtualSvcPath = "testdata/bookinfo-multiple-virtualservices.yaml"
		destRulePath = "testdata/bookinfo-productpage-destinationrule.yaml"
	default:
		t.Fatalf("Invalid gateway type for bookinfo")
	}

	g.ApplyConfigOrFail(
		t,
		serverNs,
		gatewayPath.LoadGatewayFileWithNamespaceOrFail(t, serverNs.Name()))

	g.ApplyConfigOrFail(
		t,
		serverNs,
		destRulePath.LoadWithNamespaceOrFail(t, serverNs.Name()),
		virtualSvcPath.LoadWithNamespaceOrFail(t, serverNs.Name()))
	// Restore the bookinfo root to original value.
	env.BookInfoRoot = originBookInfoRoot
}

// RunTestMultiMtlsGateways deploys multiple mTLS gateways with SDS enabled, and creates kubernetes that store
// private key, server certificate and CA certificate for each mTLS gateway. Verifies that all gateways are able to terminate
// mTLS connections successfully.
func RunTestMultiMtlsGateways(t *testing.T, ctx framework.TestContext,
	inst istio.Instance, g galley.Instance) { // nolint:interfacer
	t.Helper()

	CreateIngressKubeSecret(t, ctx, credNames, ingress.Mtls, IngressCredentialA)
	defer DeleteIngressKubeSecret(t, ctx, credNames)
	DeployBookinfo(t, ctx, g, MultiMTLSGateway)

	ing := ingress.NewOrFail(t, ctx, ingress.Config{
		Istio: inst,
	})
	tlsContext := TLSContext{
		CaCert:     CaCertA,
		PrivateKey: TLSClientKeyA,
		Cert:       TLSClientCertA,
	}
	callType := ingress.Mtls

	for _, h := range hosts {
		err := VisitProductPage(ing, h, callType, tlsContext, 30*time.Second,
			ExpectedResponse{ResponseCode: 200, ErrorMessage: ""}, t)
		if err != nil {
			t.Fatalf("unable to retrieve 200 from product page at host %s: %v", h, err)
		}
	}
}

// RunTestMultiTLSGateways deploys multiple TLS gateways with SDS enabled, and creates kubernetes that store
// private key and server certificate for each TLS gateway. Verifies that all gateways are able to terminate
// SSL connections successfully.
func RunTestMultiTLSGateways(t *testing.T, ctx framework.TestContext,
	inst istio.Instance, g galley.Instance) { // nolint:interfacer
	t.Helper()

	CreateIngressKubeSecret(t, ctx, credNames, ingress.TLS, IngressCredentialA)
	defer DeleteIngressKubeSecret(t, ctx, credNames)
	DeployBookinfo(t, ctx, g, MultiTLSGateway)

	ing := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst})
	tlsContext := TLSContext{
		CaCert: CaCertA,
	}
	callType := ingress.TLS

	for _, h := range hosts {
		err := VisitProductPage(ing, h, callType, tlsContext, 30*time.Second,
			ExpectedResponse{ResponseCode: 200, ErrorMessage: ""}, t)
		if err != nil {
			t.Fatalf("unable to retrieve 200 from product page at host %s: %v", h, err)
		}
	}
}
