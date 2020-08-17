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
	"fmt"
	"io/ioutil"
	"path"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/tests/integration/security/util/dir"
	"istio.io/istio/tests/util"
	"istio.io/pkg/log"
)

// DumpCertFromSidecar gets the certificate output from openssl s-client command.
func DumpCertFromSidecar(ns namespace.Instance, fromSelector, fromContainer, connectTarget string) (string, error) {
	retry := util.Retrier{
		BaseDelay: 10 * time.Second,
		Retries:   3,
		MaxDelay:  30 * time.Second,
	}

	fromPod, err := dir.GetPodName(ns, fromSelector)
	if err != nil {
		return "", fmt.Errorf("err getting the pod from pod name: %v", err)
	}

	var out string
	retryFn := func(_ context.Context, i int) error {
		execCmd := fmt.Sprintf(
			"kubectl exec %s -c %s -n %s -- openssl s_client -showcerts -alpn istio -connect %s",
			fromPod, fromContainer, ns.Name(), connectTarget)
		out, err = shell.Execute(false, execCmd)
		if !strings.Contains(out, "-----BEGIN CERTIFICATE-----") {
			return fmt.Errorf("the output doesn't contain certificate: %v", out)
		}
		return nil
	}

	if _, err := retry.Retry(context.Background(), retryFn); err != nil {
		return "", fmt.Errorf("get cert retry failed with err: %v", err)
	}
	return out, nil
}

// CreateCASecret creates a k8s secret "cacerts" to store the CA key and cert.
func CreateCASecret(ctx resource.Context) error {
	name := "cacerts"
	systemNs, err := namespace.ClaimSystemNamespace(ctx)
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

	cluster := ctx.Environment().(*kube.Environment).KubeClusters[0]
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

	if _, err := cluster.CoreV1().Secrets(systemNs.Name()).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			if _, err := cluster.CoreV1().Secrets(systemNs.Name()).Update(context.TODO(), secret, metav1.UpdateOptions{}); err != nil {
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
	kEnv := ctx.Environment().(*kube.Environment)
	err = kEnv.KubeClusters[0].CoreV1().ConfigMaps(systemNs.Name()).Delete(context.TODO(), configMapName,
		metav1.DeleteOptions{})
	if err == nil {
		log.Infof("configmap %v is deleted", configMapName)
	} else {
		log.Infof("configmap %v may not exist and the deletion returns err (%v)",
			configMapName, err)
	}
	return nil
}

func ReadSampleCertFromFile(f string) ([]byte, error) {
	b, err := ioutil.ReadFile(path.Join(env.IstioSrc, "samples/certs", f))
	if err != nil {
		return nil, err
	}
	return b, nil
}

// CreateCustomEgressSecret creates a k8s secret "cacerts" to store egress gateways CA key and cert.
func CreateCustomEgressSecret(ctx resource.Context) error {
	name := "egress-gw-cacerts"
	systemNs, err := namespace.ClaimSystemNamespace(ctx)
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

	kubeAccessor := ctx.Environment().(*kube.Environment).KubeClusters[0]
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

	_, err = kubeAccessor.CoreV1().Secrets(systemNs.Name()).Create(context.TODO(), secret, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func ReadCustomCertFromFile(f string) ([]byte, error) {
	b, err := ioutil.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs/dns", f))
	if err != nil {
		return nil, err
	}
	return b, nil
}
