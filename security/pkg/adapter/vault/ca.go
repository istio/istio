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

// Package vault provides adapter to connect to vault server.
package vault

import (
	"fmt"
	"time"

	"istio.io/istio/security/pkg/pki/util"
)

/*
// Config for prototyping purpose
const (
	istioCaMountPoint   = "istio_ca"
	istioCaDescription  = "Istio CA"
	configCaKeyCertPath = "istio_ca/config/ca"
	workloadRolePath    = "istio_ca/roles/workload_role"
	signCsrPath         = "istio_ca/sign-verbatim"
)

// Config for prototyping purpose
// TODO (lei-tang): move the these config to ca_test.go
const (
	vaultAddrForTesting = "http://127.0.0.1:8200"
	tokenForTesting     = "myroot"
	testCAKeyCertFile   = "testdata/istio_ca.pem"
	testCsrFile         = "testdata/workload-1.csr"
)
*/

// CA connects to Vault to sign certificates.
type CA struct {
}

// Sign takes a PEM-encoded CSR and returns a signed certificate. If the CA is a multicluster CA,
// the signed certificate is a CA certificate (CA:TRUE in X509v3 Basic Constraints), otherwise, it is a workload
// certificate.
func (v *CA) Sign(csrPEM []byte, ttl time.Duration) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

// SignCAServerCert signs the certificate for the Istio CA server (to serve the CSR, etc).
func (v *CA) SignCAServerCert(csrPEM []byte, ttl time.Duration) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetKeyCertBundle returns the KeyCertBundle for the CA.
func (v *CA) GetKeyCertBundle() util.KeyCertBundle {
	return nil
}

/*
// Get the connection to a Vault server and set the token for the connection.
// vaultAddr: the address of the Vault server (e.g., "http://127.0.0.1:8200").
// token: used for authentication.
func getVaultConnection(vaultAddr string, token string) (*api.Client, error) {
	config := api.DefaultConfig()
	config.Address = vaultAddr

	client, err := api.NewClient(config)
	if err != nil {
		log.Errorf("NewClient() failed (error %v)", err)
		return nil, err
	}

	//Simply sets the token for future requests without actual authentication
	client.SetToken(token)
	return client, nil
}

// Mount the Vault PKI.
// caMountPoint: the mount point for CA (e.g., "istio_ca")
// caDescription: a description for CA (e.g., "Istio CA")
func mountVaultPki(client *api.Client, caMountPoint string, caDescription string) error {
	var mountInput api.MountInput
	mountInput.Description = caDescription
	mountInput.Type = "pki"
	err := client.Sys().Mount(caMountPoint, &mountInput)
	if err != nil {
		log.Errorf("Mount() failed (error %v)", err)
		return err
	}
	return nil
}

// Set the workload role that issues certs with the given max-TTL and number of key bits.
// rolePath: the path to the workload role (e.g., "istio_ca/roles/workload_role")
// maxTtl:  the max life time of a workload cert (e.g., "1h")
// keyBits:  the number of bits for the key of a workload cert (e.g., 2048)
func setWorkloadRole(client *api.Client, rolePath string, maxTTL string, keyBits int) error {
	m := map[string]interface{}{
		"max_ttl":           maxTTL,
		"key_bits":          keyBits,
		"enforce_hostnames": false,
		"allow_any_name":    true,
	}

	_, err := client.Logical().Write(rolePath, m)
	if err != nil {
		log.Errorf("Write() failed (error %v)", err)
		return err
	}
	return nil
}

// Set the certificate and the private key of the CA.
// caConfigPath: the path for configuring the CA (e.g., "istio_ca/config/ca")
// keyCert: the private key and the public certificate of the CA
func setCaKeyCert(client *api.Client, caConfigPath string, keyCert string) (*api.Secret, error) {
	m := map[string]interface{}{
		"pem_bundle": keyCert,
	}

	res, err := client.Logical().Write(caConfigPath, m)
	if err != nil {
		log.Errorf("Write() failed (error %v)", err)
		return nil, err
	}
	return res, nil
}

// Sign a CSR and return the signed certificate.
// csrPath: the path for signing a CSR (e.g., "istio_ca/sign-verbatim")
// csr: the CSR to be signed
func signCsr(client *api.Client, csrPath string, csr string) (*api.Secret, error) {
	m := map[string]interface{}{
		"name":                "workload_role",
		"format":              "pem",
		"use_csr_common_name": true,
		"csr":                 csr,
	}

	res, err := client.Logical().Write(csrPath, m)
	if err != nil {
		log.Errorf("Write() failed (error %v)", err)
		return nil, err
	}
	return res, nil
}

//RunProtoTypeSignCsrFlow runs a prototyping signCsr flow, includes:
//- Create a connection to Vault
//- Mount Vault PKI
//- Set CA signing key and cert
//- Set workload role for issuing certificates
//- Sign CSR and print the certificate signed
func RunProtoTypeSignCsrFlow() error {
	client, err := getVaultConnection(vaultAddrForTesting, tokenForTesting)
	if err != nil {
		log.Errorf("getVaultConnection() failed (error %v)", err)
		return err
	}

	err = mountVaultPki(client, istioCaMountPoint, istioCaDescription)
	if err != nil {
		log.Errorf("mountVaultPki() failed (error %v)", err)
		return err
	}

	keyCert, err := ioutil.ReadFile(testCAKeyCertFile)
	if err != nil {
		log.Errorf("ReadFile() failed (error %v)", err)
		return err
	}
	_, err = setCaKeyCert(client, configCaKeyCertPath, string(keyCert))
	if err != nil {
		log.Errorf("setCaKeyCert() failed (error %v)", err)
		return err
	}

	err = setWorkloadRole(client, workloadRolePath, "1h", 2048)
	if err != nil {
		log.Errorf("setWorkloadRole() failed (error %v)", err)
		return err
	}

	testCsr, err := ioutil.ReadFile(testCsrFile)
	if err != nil {
		log.Errorf("ReadFile() failed (error %v)", err)
		return err
	}
	res, err := signCsr(client, signCsrPath, string(testCsr))
	if err != nil {
		log.Errorf("signCsr() failed (error %v)", err)
		return err
	}
	log.Info("The certificate generated from CSR is :")
	//Print the certificate
	log.Infof("%v", res.Data["certificate"])
	return nil
}
*/
