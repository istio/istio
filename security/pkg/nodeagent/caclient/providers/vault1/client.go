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

package caclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"

	"github.com/hashicorp/vault/api"
	"istio.io/istio/pkg/log"
	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
)

type vaultClient1 struct {
	enableTLS bool

	vaultAddr        string
	vaultLoginRole   string
	vaultLoginPath   string
	vaultSignCsrPath string

	client *api.Client
}

// NewVaultClient create a CA client for the Vault provider 1.
func NewVaultClient1(tls bool,
	vaultAddr, vaultLoginRole, vaultLoginPath, vaultSignCsrPath string) (caClientInterface.Client, error) {
	c := &vaultClient1{
		enableTLS:        tls,
		vaultAddr:        vaultAddr,
		vaultLoginRole:   vaultLoginRole,
		vaultLoginPath:   vaultLoginPath,
		vaultSignCsrPath: vaultSignCsrPath,
	}

	var client *api.Client
	var err error
	if tls {
		client, err = createVaultClientTLS(vaultAddr)
	} else {
		client, err = createVaultClient(vaultAddr)
	}
	if err != nil {
		return nil, err
	}
	c.client = client

	return c, nil
}

// CSR Sign calls Vault to sign a CSR.
func (c *vaultClient1) CSRSign(ctx context.Context, csrPEM []byte, token string,
	certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error) {

	saToken := token
	token, err := loginVaultK8sAuthMethod(c.client, c.vaultLoginPath, c.vaultLoginRole, string(saToken[:]))
	if err != nil {
		log.Errorf("Failed to login Vault: %v", err)
		return nil, fmt.Errorf("Failed to login Vault: %v", err)
	}
	c.client.SetToken(token)
	_, certChain, err := signCsrByVault(c.client, c.vaultSignCsrPath, csrPEM)
	if err != nil {
		log.Errorf("Failed to sign CSR: %v", err)
		return nil, fmt.Errorf("Failed to sign CSR: %v", err)
	}

	if len(certChain) <= 1 {
		log.Errorf("Certificate chain length is %d, expected more than 1", len(certChain))
		return nil, fmt.Errorf("Invalid cert. chain in the response")
	}

	return certChain, nil
}

// createVaultClient creates a client to a Vault server
// vaultAddr: the address of the Vault server (e.g., "http://127.0.0.1:8200").
func createVaultClient(vaultAddr string) (*api.Client, error) {
	config := api.DefaultConfig()
	config.Address = vaultAddr

	client, err := api.NewClient(config)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// createVaultClientTLS creates a client to a Vault server
// vaultAddr: the address of the Vault server (e.g., "https://127.0.0.1:8200").
func createVaultClientTLS(vaultAddr string) (*api.Client, error) {
	// Load the system default root certificates.
	pool, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("could not get SystemCertPool: %v", err)
		return nil, fmt.Errorf("could not get SystemCertPool: %v", err)
	}
	tlsConfig := &tls.Config{
		RootCAs: pool,
	}

	transport := &http.Transport{TLSClientConfig: tlsConfig}
	httpClient := &http.Client{Transport: transport}

	config := api.DefaultConfig()
	config.Address = vaultAddr
	config.HttpClient = httpClient

	client, err := api.NewClient(config)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// loginVaultK8sAuthMethod logs into the Vault k8s auth method with the service account and
// returns the auth client token.
// loginPath: the path of the login
// role: the login role
// jwt: the service account used for login
func loginVaultK8sAuthMethod(client *api.Client, loginPath, role, sa string) (string, error) {
	resp, err := client.Logical().Write(
		loginPath,
		map[string]interface{}{
			"jwt":  sa,
			"role": role,
		})

	if err != nil {
		return "", err
	}
	return resp.Auth.ClientToken, nil
}

// signCsrByVault signs the CSR and return the signed certifcate and the CA certificate chain
// Return the signed certificate and the CA certificate chain when succeed.
// client: the Vault client
// csrSigningPath: the path for signing a CSR
// csr: the CSR to be signed, in pem format
func signCsrByVault(client *api.Client, csrSigningPath string, csr []byte) ([]byte, []string, error) {
	m := map[string]interface{}{
		"format": "pem",
		"csr":    string(csr[:]),
	}
	res, err := client.Logical().Write(csrSigningPath, m)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to post to %v: %v", csrSigningPath, err)
	}
	//Extract the certificate and the certificate chain
	certificate, ok := res.Data["certificate"]
	if !ok {
		return nil, nil, fmt.Errorf("no certificate in the CSR response")
	}
	cert, ok := certificate.(string)
	if !ok {
		return nil, nil, fmt.Errorf("the certificate in the CSR response is not a string")
	}
	caChain, ok := res.Data["ca_chain"]
	if !ok {
		return nil, nil, fmt.Errorf("no certificate chain in the CSR response")
	}
	chain, ok := caChain.([]interface{})
	if !ok {
		return nil, nil, fmt.Errorf("the certificate chain in the CSR response is of unexpected format")
	}
	var certChain []string
	for _, c := range chain {
		_, ok := c.(string)
		if !ok {
			return nil, nil, fmt.Errorf("the certificate in the certificate chain is not a string")
		}
		certChain = append(certChain, c.(string))
	}

	return []byte(cert), certChain, nil
}
