// Copyright 2020 Istio Authors
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

package plugincakeycert

import (
	"io/ioutil"
	"path"
	"testing"

	"istio.io/istio/pkg/test/env"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/pkg/log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	inst istio.Instance
	g    galley.Instance
	p    pilot.Instance
)

func TestMain(m *testing.M) {
	// Integration test for plugging in the CA key and certificate.
	framework.
		NewSuite("plugin_ca_key_cert_test", m).
		// k8s is required because the plugin CA key and certificate are stored in a k8s secret.
		RequireEnvironment(environment.Kube).
		RequireSingleCluster().
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&inst, nil, createCASecret)).
		Setup(func(ctx resource.Context) (err error) {
			if g, err = galley.New(ctx, galley.Config{}); err != nil {
				return err
			}
			if p, err = pilot.New(ctx, pilot.Config{
				Galley: g,
			}); err != nil {
				return err
			}
			return nil
		}).
		Run()
}

// createCASecret creates a k8s secret "cacerts" to store the CA key and cert.
func createCASecret(ctx resource.Context) error {
	name := "cacerts"
	systemNs, err := namespace.ClaimSystemNamespace(ctx)
	if err != nil {
		return err
	}

	var caCert, caKey, certChain, rootCert []byte
	if caCert, err = readCertFile("ca-cert.pem"); err != nil {
		return err
	}
	if caKey, err = readCertFile("ca-key.pem"); err != nil {
		return err
	}
	if certChain, err = readCertFile("cert-chain.pem"); err != nil {
		return err
	}
	if rootCert, err = readCertFile("root-cert.pem"); err != nil {
		return err
	}

	kubeAccessor := ctx.Environment().(*kube.Environment).KubeClusters[0]
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: systemNs.Name(),
		},
		Data: map[string][]byte{
			"ca-cert.pem":    caCert,
			"ca-key.pem":     caKey,
			"cert-chain.pem": certChain,
			"root-cert.pem":  rootCert,
		},
	}

	err = kubeAccessor.CreateSecret(systemNs.Name(), secret)
	if err != nil {
		return err
	}

	// If there is a configmap storing the CA cert from a previous
	// integration test, remove it. Ideally, CI should delete all
	// resources from a previous integration test, but sometimes
	// the resources from a previous integration test are not deleted.
	configMapName := "istio-ca-root-cert"
	kEnv := ctx.Environment().(*kube.Environment)
	err = kEnv.KubeClusters[0].DeleteConfigMap(configMapName, systemNs.Name())
	if err == nil {
		log.Infof("configmap %v is deleted", configMapName)
	} else {
		log.Infof("configmap %v may not exist and the deletion returns err (%v)",
			configMapName, err)
	}
	return nil
}

func readCertFile(f string) ([]byte, error) {
	b, err := ioutil.ReadFile(path.Join(env.IstioSrc, "samples/certs", f))
	if err != nil {
		return nil, err
	}
	return b, nil
}
