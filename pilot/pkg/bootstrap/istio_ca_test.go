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
	"io/ioutil"
	"os"
	"path"
	"testing"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/env"
)

const namespace = "istio-system"

func TestRemoteCerts(t *testing.T) {
	g := NewWithT(t)

	dir, err := ioutil.TempDir("", t.Name())
	defer removeSilent(dir)
	g.Expect(err).Should(BeNil())

	s := Server{
		kubeClient: kube.NewFakeClient(),
	}
	caOpts := &caOptions{
		Namespace: namespace,
	}

	// Should do nothing because cacerts doesn't exist.
	err = s.loadRemoteCACerts(caOpts, dir)
	g.Expect(err).Should(BeNil())

	_, err = os.Stat(path.Join(dir, "root-cert.pem"))
	g.Expect(os.IsNotExist(err)).Should(Equal(true))

	// Should load remote cacerts successfully.
	err = createCASecret(s.kubeClient)
	g.Expect(err).Should(BeNil())

	err = s.loadRemoteCACerts(caOpts, dir)
	g.Expect(err).Should(BeNil())

	expectedRoot, err := readSampleCertFromFile("root-cert.pem")
	g.Expect(err).Should(BeNil())

	g.Expect(ioutil.ReadFile(path.Join(dir, "root-cert.pem"))).Should(Equal(expectedRoot))

	// Should fail because certs already exist locally.
	err = s.loadRemoteCACerts(caOpts, dir)
	g.Expect(err).NotTo(BeNil())
}

func removeSilent(dir string) {
	_ = os.RemoveAll(dir)
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
			"ca-cert.pem":    caCert,
			"ca-key.pem":     caKey,
			"cert-chain.pem": certChain,
			"root-cert.pem":  rootCert,
		},
	}
	if _, err = secrets.Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
		return err
	}
	return nil
}

func readSampleCertFromFile(f string) ([]byte, error) {
	return ioutil.ReadFile(path.Join(env.IstioSrc, "samples/certs", f))
}
