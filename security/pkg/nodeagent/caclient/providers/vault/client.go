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

package caclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"strconv"

	"github.com/hashicorp/vault/api"

	"istio.io/istio/pkg/security"
	"istio.io/pkg/log"
)

var (
	vaultClientLog = log.RegisterScope("vault", "Vault client debugging", 0)
)

type vaultClient struct {
	enableTLS   bool
	tlsRootCert []byte

	vaultAddr        string
	vaultLoginRole   string
	vaultLoginPath   string
	vaultSignCsrPath string

	client *api.Client
}

// NewVaultClient create a CA client for the Vault provider 1.
func NewVaultClient(tls bool, tlsRootCert []byte,
	vaultAddr, vaultLoginRole, vaultLoginPath, vaultSignCsrPath string) (security.Client, error) {
	c := &vaultClient{
		enableTLS:        tls,
		tlsRootCert:      tlsRootCert,
		vaultAddr:        vaultAddr,
		vaultLoginRole:   vaultLoginRole,
		vaultLoginPath:   vaultLoginPath,
		vaultSignCsrPath: vaultSignCsrPath,
	}

	var client *api.Client
	var err error
	if tls {
		client, err = createVaultTLSClient(vaultAddr, tlsRootCert)
	} else {
		client, err = createVaultClient(vaultAddr)
	}
	if err != nil {
		return nil, err
	}
	c.client = client
	vaultClientLog.Infof("created Vault client for Vault address: %s, TLS: %v", vaultAddr, tls)

	return c, nil
}

// CSR Sign calls Vault to sign a CSR.
func (c *vaultClient) CSRSign(ctx context.Context, reqID string, csrPEM []byte, saToken string,
	certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error) {
	token, err := loginVaultK8sAuthMethod(c.client, c.vaultLoginPath, c.vaultLoginRole, saToken)
	if err != nil {
		return nil, fmt.Errorf("failed to login Vault at %s: %v", c.vaultAddr, err)
	}
	c.client.SetToken(token)
	certChain, err := signCsrByVault(c.client, c.vaultSignCsrPath, certValidTTLInSec, csrPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to sign CSR: %v", err)
	}

	if len(certChain) <= 1 {
		vaultClientLog.Errorf("certificate chain length is %d, expected more than 1", len(certChain))
		return nil, fmt.Errorf("invalid certificate chain in the response")
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
		vaultClientLog.Errorf("failed to create a Vault client: %v", err)
		return nil, err
	}

	return client, nil
}

// createVaultTLSClient creates a client to a Vault server
// vaultAddr: the address of the Vault server (e.g., "https://127.0.0.1:8200").
func createVaultTLSClient(vaultAddr string, tlsRootCert []byte) (*api.Client, error) {
	// Load the system default root certificates.
	pool, err := x509.SystemCertPool()
	if err != nil {
		vaultClientLog.Errorf("could not get SystemCertPool: %v", err)
		return nil, fmt.Errorf("could not get SystemCertPool: %v", err)
	}
	if pool == nil {
		log.Info("system cert pool is nil, create a new cert pool")
		pool = x509.NewCertPool()
	}
	if len(tlsRootCert) > 0 {
		ok := pool.AppendCertsFromPEM(tlsRootCert)
		if !ok {
			return nil, fmt.Errorf("failed to append a certificate (%v) to the certificate pool", string(tlsRootCert))
		}
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
		vaultClientLog.Errorf("failed to create a Vault client: %v", err)
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
		vaultClientLog.Errorf("failed to login Vault: %v", err)
		return "", err
	}
	if resp == nil {
		vaultClientLog.Errorf("login response is nil")
		return "", fmt.Errorf("login response is nil")
	}
	if resp.Auth == nil {
		vaultClientLog.Errorf("login response auth field is nil")
		return "", fmt.Errorf("login response auth field is nil")
	}
	return resp.Auth.ClientToken, nil
}

// signCsrByVault signs the CSR and return the signed certificate and the CA certificate chain
// Return the signed certificate chain when succeed.
// client: the Vault client
// csrSigningPath: the path for signing a CSR
// csr: the CSR to be signed, in pem format
func signCsrByVault(client *api.Client, csrSigningPath string, certTTLInSec int64, csr []byte) ([]string, error) {
	m := map[string]interface{}{
		"format":               "pem",
		"csr":                  string(csr),
		"ttl":                  strconv.FormatInt(certTTLInSec, 10) + "s",
		"exclude_cn_from_sans": true,
	}
	res, err := client.Logical().Write(csrSigningPath, m)
	if err != nil {
		vaultClientLog.Errorf("failed to post to %v: %v", csrSigningPath, err)
		return nil, fmt.Errorf("failed to post to %v: %v", csrSigningPath, err)
	}
	if res == nil {
		vaultClientLog.Error("sign response is nil")
		return nil, fmt.Errorf("sign response is nil")
	}
	if res.Data == nil {
		vaultClientLog.Error("sign response has a nil Data field")
		return nil, fmt.Errorf("sign response has a nil Data field")
	}
	//Extract the certificate and the certificate chain
	certificate, ok := res.Data["certificate"]
	if !ok {
		vaultClientLog.Error("no certificate in the CSR response")
		return nil, fmt.Errorf("no certificate in the CSR response")
	}
	cert, ok := certificate.(string)
	if !ok {
		vaultClientLog.Error("the certificate in the CSR response is not a string")
		return nil, fmt.Errorf("the certificate in the CSR response is not a string")
	}
	caChain, ok := res.Data["ca_chain"]
	if !ok {
		vaultClientLog.Error("no certificate chain in the CSR response")
		return nil, fmt.Errorf("no certificate chain in the CSR response")
	}
	chain, ok := caChain.([]interface{})
	if !ok {
		vaultClientLog.Error("the certificate chain in the CSR response is of unexpected format")
		return nil, fmt.Errorf("the certificate chain in the CSR response is of unexpected format")
	}
	var certChain []string
	certChain = append(certChain, cert+"\n")
	for idx, c := range chain {
		_, ok := c.(string)
		if !ok {
			vaultClientLog.Errorf("the certificate in the certificate chain %v is not a string", idx)
			return nil, fmt.Errorf("the certificate in the certificate chain %v is not a string", idx)
		}
		certChain = append(certChain, c.(string)+"\n")
	}

	return certChain, nil
}
