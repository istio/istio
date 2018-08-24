// Copyright 2018 Istio Authors
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

	"github.com/hashicorp/vault/api"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/pki/util"
)

// CA connects to Vault to sign certificates.
type CA struct {
}

// New returns a new CA instance.
func New() (*CA, error) {
	return &CA{}, nil
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

// CreateVaultClient creates a client to a Vault server
// vaultAddr: the address of the Vault server (e.g., "http://127.0.0.1:8200").
func CreateVaultClient(vaultAddr string) (*api.Client, error) {
	config := api.DefaultConfig()
	config.Address = vaultAddr

	client, err := api.NewClient(config)
	if err != nil {
		log.Errorf("NewClient() failed (error %v)", err)
		return nil, err
	}

	return client, nil
}

// CreateVaultClientWithToken creates a client to Vault server and set the token for the connection.
// vaultAddr: the address of the Vault server (e.g., "http://127.0.0.1:8200").
// token: used for authentication.
func CreateVaultClientWithToken(vaultAddr string, token string) (*api.Client, error) {
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

// LoginKubernetesAuthMethod logs into the Kubernetes auth method with the service account and
// returns the auth client token.
// sa: the service account used for login
func LoginKubernetesAuthMethod(client *api.Client, role string, sa string) (string, error) {
	loginPath := "auth/kubernetes/login"
	resp, err := client.Logical().Write(
		loginPath,
		map[string]interface{}{
			"jwt":  sa,
			"role": role,
		})

	if err != nil {
		log.Errorf("Login failed (error %v)", err)
		return "", err
	}
	return resp.Auth.ClientToken, nil
}

// MountVaultPki mounts the Vault PKI.
// caMountPoint: the mount point for CA (e.g., "istio_ca")
// caDescription: a description for CA (e.g., "Istio CA")
func MountVaultPki(client *api.Client, caMountPoint string, caDescription string) error {
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

// SetWorkloadRole sets the workload role that issues certs with the given max-TTL and number of key bits.
// rolePath: the path to the workload role (e.g., "istio_ca/roles/workload_role")
// maxTtl:  the max life time of a workload cert (e.g., "1h")
// keyBits:  the number of bits for the key of a workload cert (e.g., 2048)
func SetWorkloadRole(client *api.Client, rolePath string, maxTTL string, keyBits int) error {
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

// SetCaKeyCert sets the certificate and the private key of the CA.
// caConfigPath: the path for configuring the CA (e.g., "istio_ca/config/ca")
// keyCert: the private key and the public certificate of the CA
func SetCaKeyCert(client *api.Client, caConfigPath string, keyCert string) (*api.Secret, error) {
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

// SignCsr signs a CSR and return the signed certificate.
// csrPath: the path for signing a CSR (e.g., "istio_ca/sign-verbatim")
// csr: the CSR to be signed
func SignCsr(client *api.Client, csrPath string, csr []byte) (*api.Secret, error) {
	m := map[string]interface{}{
		"format":              "pem",
		"use_csr_common_name": true,
		"csr": string(csr[:]),
	}

	res, err := client.Logical().Write(csrPath, m)
	if err != nil {
		log.Errorf("Write() failed (error %v)", err)
		return nil, err
	}
	return res, nil
}
