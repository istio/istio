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

package bootstrap

import (
	"context"
	"os"
	"path"
	"testing"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/security/pkg/pki/ca"
)

const namespace = "istio-system"

func TestRemoteCerts(t *testing.T) {
	g := NewWithT(t)

	dir := t.TempDir()

	s := Server{
		kubeClient: kube.NewFakeClient(),
	}
	caOpts := &caOptions{
		Namespace: namespace,
	}

	// Should do nothing because cacerts doesn't exist.
	err := s.loadCACerts(caOpts, dir)
	g.Expect(err).Should(BeNil())

	_, err = os.Stat(path.Join(dir, "root-cert.pem"))
	g.Expect(os.IsNotExist(err)).Should(Equal(true))

	// Should load remote cacerts successfully.
	err = createCASecret(s.kubeClient)
	g.Expect(err).Should(BeNil())

	err = s.loadCACerts(caOpts, dir)
	g.Expect(err).Should(BeNil())

	expectedRoot, err := readSampleCertFromFile("root-cert.pem")
	g.Expect(err).Should(BeNil())

	g.Expect(os.ReadFile(path.Join(dir, "root-cert.pem"))).Should(Equal(expectedRoot))

	// Should do nothing because certs already exist locally.
	err = s.loadCACerts(caOpts, dir)
	g.Expect(err).Should(BeNil())
}

func createCASecret(client kube.Client) error {
	var caCert, caKey, certChain, rootCert []byte
	var err error
	if caCert, err = readSampleCertFromFile("ca-cert.pem"); err != nil {
		return err
	}
	if caKey, err = readSampleCertFromFile("ca-key.pem"); err != nil {
		return err
	}
	if certChain, err = readSampleCertFromFile("cert-chain.pem"); err != nil {
		return err
	}
	if rootCert, err = readSampleCertFromFile("root-cert.pem"); err != nil {
		return err
	}

	secrets := client.Kube().CoreV1().Secrets(namespace)
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "cacerts",
		},
		Data: map[string][]byte{
			ca.CACertFile:       caCert,
			ca.CAPrivateKeyFile: caKey,
			ca.CertChainFile:    certChain,
			ca.RootCertFile:     rootCert,
		},
	}
	if _, err = secrets.Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
		return err
	}
	return nil
}

func readSampleCertFromFile(f string) ([]byte, error) {
	return os.ReadFile(path.Join(env.IstioSrc, "samples/certs", f))
}
