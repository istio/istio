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
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
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

var certMakefile = filepath.Join(env.IstioSrc, "install/tools/certs/Makefile")

func mintCerts(d string, namespace string) error {
	c := exec.Command("make", "-f", certMakefile, namespace+"-certs-selfSigned")
	c.Dir = d
	o, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("creating certs output: %v, err: %v", string(o), err)
	}
	return nil
}

// CreateCASecret creates a k8s secret "cacerts" to store the CA key and cert.
func CreateCASecret(ctx resource.Context, workloadNamespace string) error {
	name := "cacerts"
	systemNs, err := namespace.ClaimSystemNamespace(ctx)
	if err != nil {
		return err
	}
	if workloadNamespace == "" {
		workloadNamespace = systemNs.Name()
	}

	dir, err := ctx.CreateTmpDirectory("certificates")
	if err != nil {
		return err
	}

	if err := mintCerts(dir, workloadNamespace); err != nil {
		return err
	}

	base := filepath.Join(dir, workloadNamespace)
	var caCert, caKey, certChain, rootCert, workloadCert, workloadKey []byte
	if caCert, err = readCert(base, "selfSigned-ca-cert.pem"); err != nil {
		return err
	}
	if caKey, err = readCert(base, "selfSigned-ca-key.pem"); err != nil {
		return err
	}
	if certChain, err = readCert(base, "selfSigned-ca-cert-chain.pem"); err != nil {
		return err
	}
	if rootCert, err = readCert(base, "root-cert.pem"); err != nil {
		return err
	}

	if workloadCert, err = readCert(base, "selfSigned-workload-cert.pem"); err != nil {
		return err
	}
	if workloadKey, err = readCert(base, "key.pem"); err != nil {
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

	err = kubeAccessor.DeleteSecret(systemNs.Name(), name)
	if err == nil {
		log.Infof("secret %v is deleted", name)
	}
	err = kubeAccessor.CreateSecret(systemNs.Name(), secret)
	if err != nil {
		return err
	}

	workloadSecretName := "workload-certs"
	workloadSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workloadSecretName,
			Namespace: workloadNamespace,
		},
		Data: map[string][]byte{
			"cert-chain.pem": workloadCert,
			"key.pem":        workloadKey,
			"root-cert.pem":  caCert,
		},
	}

	err = kubeAccessor.DeleteSecret(workloadNamespace, workloadSecretName)
	if err == nil {
		log.Infof("secret %v is deleted", workloadSecretName)
	}
	err = kubeAccessor.CreateSecret(workloadNamespace, workloadSecret)
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

func readCert(base string, f string) ([]byte, error) {
	b, err := ioutil.ReadFile(path.Join(base, f))
	if err != nil {
		return nil, err
	}
	return b, nil
}

func ReadSampleCertFromFile(f string) ([]byte, error) {
	return readCert(path.Join(env.IstioSrc, "tests/testdata/certs/pilot"), f)
}
