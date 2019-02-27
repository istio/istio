// Copyright 2019 Istio Authors
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

package ca

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/probe"
	ca "istio.io/istio/pkg/webhook/ca/model"
	"istio.io/istio/pkg/webhook/ca/util"
	"istio.io/istio/pkg/webhook/k8s/controller"
)

var (
	// certChainID is the ID/name for the certificate chain file.
	certChainID = "cert-chain.pem"
	// privateKeyID is the ID/name for the private key file.
	privateKeyID = "key.pem"
	// rootCertID is the ID/name for the CA root certificate file.
	rootCertID = "root-cert.pem"

	secretNamePrefix = "istio."
)

//GenerateSelfSignedCA generated self signed ca if istio.istio-galley-service-account does not exist
func GenerateSelfSignedCA(caArgs *ca.Args, workloadCertArgs *ca.WorkloadCertArgs, kubeConfig,
	namespace, svcName, svcAccountName string) error {

	clientset, err := kube.CreateClientset(kubeConfig, "")
	if err != nil {
		return fmt.Errorf("could not create k8s clientset: %v", err)
	}
	if !secretIsAlreadyExists(clientset, namespace, svcAccountName) {
		err := createSecret(clientset, caArgs, workloadCertArgs, namespace, svcName, svcAccountName)
		if err != nil {
			return fmt.Errorf("generate secret failed: %v", err)
		}
	} else {
		return generateCAFilesAndWatchSecret(clientset, caArgs, workloadCertArgs,
			namespace, svcName, svcAccountName)
	}
	return nil
}

func createSecret(clientset *kubernetes.Clientset, caArgs *ca.Args, workloadCertArgs *ca.WorkloadCertArgs,
	namespace, svcName, svcAccountName string) (err error) {
	options := util.CertOptions{
		TTL:          caArgs.RequestedCertTTL,
		Org:          caArgs.Org,
		IsCA:         caArgs.ForCA,
		IsSelfSigned: true,
		RSAKeySize:   caArgs.RSAKeySize,
	}

	pemCert, pemKey, ckErr := util.GenCertKeyFromOptions(options)
	if ckErr != nil {
		return fmt.Errorf("unable to generate CA cert and key for self-signed CA (%v)", ckErr)
	}

	caKeyCertBundle, err := util.NewVerifiedKeyCertBundleFromPem(pemCert, pemKey, nil, pemCert)
	if err != nil {
		return fmt.Errorf("failed to create CA KeyCertBundle (%v)", err)
	}

	err = createCAFiles(caArgs, pemCert, pemKey, pemCert)
	if err != nil {
		return err
	}

	return watchSecret(clientset, caArgs, workloadCertArgs,
		caKeyCertBundle, namespace, svcName, svcAccountName)

}

func watchSecret(clientset *kubernetes.Clientset, caArgs *ca.Args, workloadCertArgs *ca.WorkloadCertArgs,
	caKeyCertBundle util.KeyCertBundle, namespace, svcName, svcAccountName string) error {

	ca := &ca.WebhookCA{
		CertTTL:       caArgs.CertTTL,
		MaxCertTTL:    caArgs.MaxCertTTL,
		KeyCertBundle: caKeyCertBundle,
		LivenessProbe: probe.NewProbe(),
	}

	webhooks := make(map[string]controller.DNSNameEntry)
	webhooks[svcAccountName] = controller.DNSNameEntry{
		ServiceName: svcName,
		Namespace:   namespace,
	}

	sc, err := controller.NewSecretController(ca,
		workloadCertArgs.WorkloadCertTTL,
		workloadCertArgs.WorkloadCertGracePeriodRatio,
		workloadCertArgs.WorkloadCertMinGracePeriod, false,
		clientset.CoreV1(), caArgs.ForCA, namespace, webhooks)

	if err != nil {
		return fmt.Errorf("failed to create secret controller: %v", err)
	}

	stopCh := make(chan struct{})
	sc.Run(stopCh)

	return nil
}

func generateCAFilesAndWatchSecret(clientset *kubernetes.Clientset, caArgs *ca.Args,
	workloadCertArgs *ca.WorkloadCertArgs, namespace, svcName, svcAccountName string) error {

	secret, scrtErr := clientset.CoreV1().Secrets(namespace).Get(secretNamePrefix+svcAccountName, metav1.GetOptions{})
	if scrtErr != nil {
		return fmt.Errorf("failed to get secret (%v): %v", secret, scrtErr)
	}

	pemCert := secret.Data[certChainID]
	pemKey := secret.Data[privateKeyID]
	pemRootCert := secret.Data[rootCertID]

	err := createCAFiles(caArgs, pemCert, pemKey, pemRootCert)
	if err != nil {
		return err
	}

	caKeyCertBundle, err := util.NewVerifiedKeyCertBundleFromPem(pemCert, pemKey, nil, pemRootCert)
	if err != nil {
		return fmt.Errorf("failed to create CA KeyCertBundle (%v)", err)
	}

	return watchSecret(clientset, caArgs, workloadCertArgs,
		caKeyCertBundle, namespace, svcName, svcAccountName)

}

func createCAFiles(caArgs *ca.Args, certBytes, keyBytes, rootCertBytes []byte) error {

	err := createFile(caArgs.CertFile, certBytes)
	if err != nil {
		return fmt.Errorf("failed to write Certificate (%v)", err)
	}

	err = createFile(caArgs.KeyFile, keyBytes)
	if err != nil {
		return fmt.Errorf("failed to write private key (%v)", err)
	}

	err = createFile(caArgs.RootCertFile, rootCertBytes)
	if err != nil {
		return fmt.Errorf("failed to write CA KeyCertBundle (%v)", err)
	}

	return nil
}

func secretIsAlreadyExists(clientset *kubernetes.Clientset, namespace, svcAccountName string) bool {

	_, scrtErr := clientset.CoreV1().Secrets(namespace).Get(secretNamePrefix+svcAccountName, metav1.GetOptions{})
	if scrtErr != nil {
		if errors.IsNotFound(scrtErr) {
			return false
		}
	}
	return true
}

func createFile(filename string, content []byte) (err error) {
	_, err = os.Stat(filename)
	// create file if not exists
	if os.IsNotExist(err) {
		path, _ := filepath.Split(filename)
		os.Mkdir(path, 0644)

		err := ioutil.WriteFile(filename, content, 0400)
		if err != nil {
			return err
		}
	}
	return nil
}
