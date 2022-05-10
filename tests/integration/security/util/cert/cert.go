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

package cert

import (
	"context"
	"encoding/json"
	"os"
	"path"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/pkg/log"
)

// DumpCertFromSidecar gets the certificates served by the destination.
func DumpCertFromSidecar(t test.Failer, from echo.Instance, to echo.Target, port string) []string {
	result := from.CallOrFail(t, echo.CallOptions{
		To: to,
		Port: echo.Port{
			Name: port,
		},
		Scheme: scheme.TLS,
		TLS: echo.TLS{
			Alpn: []string{"istio"},
		},
	})
	if result.Responses.Len() != 1 {
		t.Fatalf("dump cert failed, no responses")
	}
	var certs []string
	for _, rr := range result.Responses[0].Body() {
		var s string
		if err := json.Unmarshal([]byte(rr), &s); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		certs = append(certs, s)
	}
	return certs
}

// CreateCASecret creates a k8s secret "cacerts" to store the CA key and cert.
func CreateCASecret(ctx resource.Context) error {
	name := "cacerts"
	systemNs, err := istio.ClaimSystemNamespace(ctx)
	if err != nil {
		return err
	}

	var caCert, caKey, certChain, rootCert []byte
	if caCert, err = ReadSampleCertFromFile("ca-cert.pem"); err != nil {
		return err
	}
	if caKey, err = ReadSampleCertFromFile("ca-key.pem"); err != nil {
		return err
	}
	if certChain, err = ReadSampleCertFromFile("cert-chain.pem"); err != nil {
		return err
	}
	if rootCert, err = ReadSampleCertFromFile("root-cert.pem"); err != nil {
		return err
	}

	for _, cluster := range ctx.AllClusters() {
		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: systemNs.Name(),
			},
			Data: map[string][]byte{
				ca.CACertFile:       caCert,
				ca.CAPrivateKeyFile: caKey,
				ca.CertChainFile:    certChain,
				ca.RootCertFile:     rootCert,
			},
		}

		if _, err := cluster.Kube().CoreV1().Secrets(systemNs.Name()).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
			if errors.IsAlreadyExists(err) {
				if _, err := cluster.Kube().CoreV1().Secrets(systemNs.Name()).Update(context.TODO(), secret, metav1.UpdateOptions{}); err != nil {
					return err
				}
			} else {
				return err
			}
		}

		// If there is a configmap storing the CA cert from a previous
		// integration test, remove it. Ideally, CI should delete all
		// resources from a previous integration test, but sometimes
		// the resources from a previous integration test are not deleted.
		configMapName := "istio-ca-root-cert"
		err = cluster.Kube().CoreV1().ConfigMaps(systemNs.Name()).Delete(context.TODO(), configMapName,
			metav1.DeleteOptions{})
		if err == nil {
			log.Infof("configmap %v is deleted", configMapName)
		} else {
			log.Infof("configmap %v may not exist and the deletion returns err (%v)",
				configMapName, err)
		}
	}

	return nil
}

func ReadSampleCertFromFile(f string) ([]byte, error) {
	filename := f
	if !path.IsAbs(filename) {
		filename = path.Join(env.IstioSrc, "samples/certs", f)
	}
	b, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// CreateCustomEgressSecret creates a k8s secret "cacerts" to store egress gateways CA key and cert.
func CreateCustomEgressSecret(ctx resource.Context) error {
	name := "egress-gw-cacerts"
	systemNs, err := istio.ClaimSystemNamespace(ctx)
	if err != nil {
		return err
	}

	var caKey, certChain, rootCert, fakeRootCert []byte
	if caKey, err = ReadCustomCertFromFile("key.pem"); err != nil {
		return err
	}
	if certChain, err = ReadCustomCertFromFile("cert-chain.pem"); err != nil {
		return err
	}
	if rootCert, err = ReadCustomCertFromFile("root-cert.pem"); err != nil {
		return err
	}
	if fakeRootCert, err = ReadCustomCertFromFile("fake-root-cert.pem"); err != nil {
		return err
	}

	kubeAccessor := ctx.Clusters().Default()
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: systemNs.Name(),
		},
		Data: map[string][]byte{
			"key.pem":            caKey,
			"cert-chain.pem":     certChain,
			"root-cert.pem":      rootCert,
			"fake-root-cert.pem": fakeRootCert,
		},
	}

	_, err = kubeAccessor.Kube().CoreV1().Secrets(systemNs.Name()).Create(context.TODO(), secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			_, err = kubeAccessor.Kube().CoreV1().Secrets(systemNs.Name()).Update(context.TODO(), secret, metav1.UpdateOptions{})
			return err
		}
		return err
	}

	return nil
}

func ReadCustomCertFromFile(f string) ([]byte, error) {
	b, err := os.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs/dns", f))
	if err != nil {
		return nil, err
	}
	return b, nil
}
