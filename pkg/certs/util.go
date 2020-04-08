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
package cert

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"k8s.io/client-go/kubernetes"

	"istio.io/istio/security/pkg/cmd"
	k8ssecret "istio.io/istio/security/pkg/k8s/secret"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// CASecret defines plugged-in secret name
	PlugInSecret = "cacerts"
	// RootCertName defines plugged-in root cert file name
	RootCertName = "root-cert.pem"
	// CAKeyName defines plugged-in ca private key file name
	CAKeyName = "ca-key.pem"
	// CACertName  defines plugged-in ca cert file name
	CACertName = "ca-cert.pem"
	//CertChainName  defines plugged-in cert-chain file name
	CertChainName = "cert-chain.pem"

	caKeySize = 2048
)

// PlugInProvisionedCert plugin external CAs into Istio

func PlugInProvisionedCert(client kubernetes.Interface, certDir, ns string, dryRun bool) error {
	stat, err := os.Stat(certDir)
	if err != nil {
		return err
	}
	if stat.Mode().Perm() != 0777 {
		return fmt.Errorf("specified certDir %q should have mode %#o, has %#o", certDir, os.ModePerm, stat.Mode())
	}
	rootCert, err := ioutil.ReadFile(filepath.Join(certDir, RootCertName))
	if err != nil {
		log.Warnf("can not read %q from %s", RootCertName, certDir)
		return err
	}
	cakey, err := ioutil.ReadFile(filepath.Join(certDir, CAKeyName))
	if err != nil {
		log.Warnf("can not read %q from %s", CAKeyName, certDir)
		return err
	}
	caCert, err := ioutil.ReadFile(filepath.Join(certDir, CACertName))
	if err != nil {
		log.Warnf("can not read %q from %s", CACertName, certDir)
		return err
	}
	certChain, err := ioutil.ReadFile(filepath.Join(certDir, CertChainName))
	if err != nil {
		log.Warnf("can not read %q from %s", CertChainName, certDir)
		return err
	}

	err = createSecret(client, ns, rootCert, cakey, caCert, certChain, dryRun)
	if err != nil {
		return err
	}
	return nil
}

//most parts are coming from security/tools/generate_cert/main.go
// GenerateCertsAndKey is used to generate key and certs
func GenerateCertsAndKey(client kubernetes.Interface, ns string, dryRun bool, outputDir, org string) error {
	_, scrtErr := client.CoreV1().Secrets(ns).Get(context.TODO(), PlugInSecret, metav1.GetOptions{})
	if scrtErr == nil {
		log.Infof(PlugInSecret+"already exists in %v, skip generating self-signed Certs and Keys", ns)
		return nil
	}
	if dryRun {
		log.Infof("dry run mode: skip generating self-signed Certs and Keys ")
		return nil
	}
	if org == "" {
		org = "istio.io"
	}
	options := util.CertOptions{
		TTL:          cmd.DefaultSelfSignedCACertTTL,
		Org:          org,
		IsCA:         true,
		IsSelfSigned: true,
		RSAKeySize:   caKeySize,
		IsDualUse:    true,
	}
	rootCertBytes, rootKeyBytes, ckErr := util.GenCertKeyFromOptions(options)
	if ckErr != nil {
		return ckErr
	}

	rootCert, err := util.ParsePemEncodedCertificate(rootCertBytes)
	if err != nil {
		return err
	}

	rootKey, err := util.ParsePemEncodedKey(rootKeyBytes)
	if err != nil {
		return err
	}
	err = saveRootCert(outputDir, rootCertBytes)
	if err != nil {
		return err
	}
	opts := util.CertOptions{
		Org:          org + "intermediate",
		TTL:          cmd.DefaultSelfSignedCACertTTL,
		SignerCert:   rootCert,
		SignerPriv:   rootKey,
		IsCA:         true,
		IsSelfSigned: false,
		RSAKeySize:   caKeySize,
	}

	intermediateCertBytes, intermediateKeyBytes, ckErr := util.GenCertKeyFromOptions(opts)
	if ckErr != nil {
		return ckErr
	}
	err = createSecret(client, ns, rootCertBytes, intermediateKeyBytes, intermediateCertBytes, intermediateCertBytes, dryRun)
	if err != nil {
		return err
	}
	opts.Org = org + ".intermediate1"
	intermediateCertBytes1, intermediateKeyBytes1, ckErr := util.GenCertKeyFromOptions(opts)
	if ckErr != nil {
		return ckErr
	}

	err = saveIntermediateCA(outputDir, "intermediateCA1", intermediateCertBytes1, intermediateKeyBytes1)
	if err != nil {
		return err
	}
	opts.Org = org + ".intermediate2"
	intermidiateCertBytes2, intermediateKeyBytes2, ckErr := util.GenCertKeyFromOptions(opts)
	if ckErr != nil {
		return ckErr
	}
	err = saveIntermediateCA(outputDir, "intermediateCA2", intermidiateCertBytes2, intermediateKeyBytes2)
	if err != nil {
		return err
	}
	return nil
}

func saveRootCert(certDir string, rootCerts []byte) error {
	rootCertDir := path.Join(certDir, "rootCert")
	// creates target folder if not already exists
	if err := os.MkdirAll(rootCertDir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %q", rootCertDir)
	}
	err := ioutil.WriteFile(rootCertDir+"/"+"root-cert.pem", rootCerts, 0644)
	if err != nil {
		return fmt.Errorf("could not write root certificate: %s", err)
	}
	return nil
}

func saveIntermediateCA(certDir, intermediateCADir string, certPem []byte, privPem []byte) error {
	intermediate := path.Join(certDir, intermediateCADir)
	if err := os.MkdirAll(intermediate, 0700); err != nil {
		return fmt.Errorf("failed to create directory %q", intermediate)
	}
	err := ioutil.WriteFile(intermediate+"/"+"ca-cert.pem", certPem, 0644)
	if err != nil {
		return fmt.Errorf("could not write intermediateCA1 certificate: %s", err)
	}
	err = ioutil.WriteFile(intermediate+"/"+"ca-key.pem", privPem, 0644)
	if err != nil {
		return fmt.Errorf("could not write intermediateCA1 key: %s", err)
	}
	return nil
}

func createSecret(client kubernetes.Interface, ns string, rootCert, cakey, caCert, certChain []byte, dryRun bool) error {
	secret := k8ssecret.BuildSecret("", PlugInSecret, ns, certChain, nil, rootCert, caCert, cakey, v1.SecretTypeOpaque)
	cmd := "dry run mode: would be running this cmd: kubectl create secret " + PlugInSecret
	if dryRun {
		log.Infof(cmd)
		return nil
	}
	if _, err := client.CoreV1().Secrets(ns).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("failed to create secret %s due to secret write error", PlugInSecret)
	}
	return nil
}
