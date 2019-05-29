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
	"strings"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

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
)

type ingressCredential struct {
	PrivateKey string
	ServerCert string
	CaCert     string
}

var IngressCredentialA = ingressCredential{
	PrivateKey: TLSServerKeyA,
	ServerCert: TLSServerCertA,
	CaCert:     CaCertA,
}
var IngressCredentialB = ingressCredential{
	PrivateKey: TLSServerKeyB,
	ServerCert: TLSServerCertB,
	CaCert:     CaCertB,
}

// CreateIngressKubeSecret reads credential names from credNames and key/cert from ingressCred,
// and creates K8s secrets for ingress gateway.
func CreateIngressKubeSecret(t *testing.T, ctx framework.TestContext, credNames []string,
	ingressType ingress.IgType, ingressCred ingressCredential) {
	// Get namespace for ingress gateway pod.
	istioCfg := istio.DefaultConfigOrFail(t, ctx)
	systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)

	if len(credNames) == 0 {
		t.Log("no credential names are specified, skip creating ingress secret")
		return
	}
	// Create Kubernetes secret for ingress gateway
	kubeAccessor := ctx.Environment().(*kube.Environment).Accessor
	for _, cn := range credNames {
		secret := createSecret(ingressType, cn, systemNS.Name(), ingressCred)
		err := kubeAccessor.CreateSecret(systemNS.Name(), secret)
		if err != nil {
			t.Errorf("Failed to create secret (error: %s)", err)
		}
	}
	// Check if Kubernetes secret is ready
	maxRetryNumber := 5
	checkRetryInterval := time.Second * 1
	for _, cn := range credNames {
		t.Logf("Check ingress Kubernetes secret %s:%s...", systemNS.Name(), cn)
		for i := 0; i < maxRetryNumber; i++ {
			_, err := kubeAccessor.GetSecret(systemNS.Name()).Get(cn, metav1.GetOptions{})
			if err != nil {
				time.Sleep(checkRetryInterval)
			} else {
				t.Logf("Secret %s:%s is ready.", systemNS.Name(), cn)
				break
			}
		}
	}
}

// createSecret creates a kubernetes secret which stores private key, server certificate for TLS ingress gateway.
// For mTLS ingress gateway, createSecret adds ca certificate into the secret object.
func createSecret(ingressType ingress.IgType, cn, ns string, ic ingressCredential) *v1.Secret {
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

// VisitProductPage makes HTTPS request to ingress gateway to visit product page
func VisitProductPage(ingress ingress.Instance, host string, timeout time.Duration, exRsp ExpectedResponse, t *testing.T) error {
	start := time.Now()
	for {
		response, err := ingress.Call("/productpage", host)
		errorMatch := true
		if err != nil {
			t.Logf("Unable to connect to product page: %v", err)
			if !strings.Contains(err.Error(), exRsp.ErrorMessage) {
				errorMatch = false
			}
		}

		status := response.Code
		if status == exRsp.ResponseCode && errorMatch {
			t.Logf("Got %d response from product page!", status)
			return nil
		} else if status != exRsp.ResponseCode {
			t.Errorf("expected response code %d but got %d", exRsp.ResponseCode, status)
		} else {
			t.Errorf("expected response error message %d but got %d", exRsp.ErrorMessage, err.Error())
		}

		if time.Since(start) > timeout {
			return fmt.Errorf("could not retrieve product page in %v: Last status: %v", timeout, status)
		}

		time.Sleep(3 * time.Second)
	}
}

// RotateSecrets deletes kubernetes secrets by name in credNames and creates same secrets using key/cert
// from ingressCred.
func RotateSecrets(t *testing.T, ctx framework.TestContext, credNames []string,
	ingressType ingress.IgType, ingressCred ingressCredential) {
	istioCfg := istio.DefaultConfigOrFail(t, ctx)
	systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)
	kubeAccessor := ctx.Environment().(*kube.Environment).Accessor
	for _, cn := range credNames {
		err := kubeAccessor.DeleteSecret(systemNS.Name(), cn)
		if err != nil {
			t.Errorf("Failed to create secret (error: %s)", err)
		}
	}
	// Check if Kubernetes secret is deleted
	maxRetryNumber := 5
	checkRetryInterval := time.Second * 1
	for _, cn := range credNames {
		t.Logf("Check ingress Kubernetes secret %s:%s...", systemNS.Name(), cn)
		for i := 0; i < maxRetryNumber; i++ {
			_, err := kubeAccessor.GetSecret(systemNS.Name()).Get(cn, metav1.GetOptions{})
			if err != nil {
				t.Logf("Secret %s:%s is deleted", systemNS.Name(), cn)
				break
			} else {
				t.Logf("Secret %s:%s still exists.", systemNS.Name(), cn)
				time.Sleep(checkRetryInterval)
			}
		}
	}

	CreateIngressKubeSecret(t, ctx, credNames, ingressType, ingressCred)
}
