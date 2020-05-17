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
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	vaultapi "github.com/hashicorp/vault/api"

	"istio.io/istio/security/pkg/util"
	"istio.io/pkg/log"
)

const (
	certKeyInCACertResp        = "certificate"
	certKeyInCertSignResp      = "certificate"
	caChainKeyInCertSignResp   = "ca_chain"
	issuingCAKeyInCertSignResp = "issuing_ca"
	jwtKeyInLoginReq           = "jwt"
	roleKeyInLoginReq          = "role"
)

var (
	vaultClientLog = log.RegisterScope("vault", "Vault client debugging", 0)
)

// VaultClient is a client for interaction with Vault.
type VaultClient struct {
	enableTLS       bool
	vaultAddr       string
	tlsRootCertPath string
	jwtPath         string
	loginRole       string
	loginPath       string
	signCsrPath     string
	caCertPath      string

	client    *vaultapi.Client
	jwtLoader *util.JwtLoader
}

// NewVaultClientWithConfig creates a CA client for the Vault PKI. It takes a single format config string.
func NewVaultClientWithConfig(config string) (*VaultClient, error) {
	params := strings.Split(config, ";")
	if len(params) != 7 {
		return nil, fmt.Errorf(
			"error processing config for Vault. Expected 'backend_ca_addr;tls_cert_path;jwt_path;role;"+
				"auth_path;sign_csr_path;ca_cert_path', but got '%s' which has %d segments", config, len(params))
	}
	vaultAddr := params[0]
	tlsRootCertPath := params[1]
	jwtPath := params[2]
	vaultLoginRole := params[3]
	vaultLoginPath := params[4]
	vaultSignCsrPath := params[5]
	vaultCACertPath := params[6]

	return NewVaultClient(
		vaultAddr, tlsRootCertPath, jwtPath, vaultLoginRole, vaultLoginPath, vaultSignCsrPath, vaultCACertPath)
}

// NewVaultClient creates a CA client for the Vault PKI.
func NewVaultClient(vaultAddr, tlsRootCertPath, jwtPath, loginRole, loginPath,
	signCsrPath, caCertPath string) (*VaultClient, error) {
	c := &VaultClient{
		enableTLS:       true,
		vaultAddr:       vaultAddr,
		tlsRootCertPath: tlsRootCertPath,
		jwtPath:         jwtPath,
		loginRole:       loginRole,
		loginPath:       loginPath,
		signCsrPath:     signCsrPath,
		caCertPath:      caCertPath,
	}
	if strings.HasPrefix(c.vaultAddr, "http:") {
		c.enableTLS = false
	}

	jwtLoader, tlErr := util.NewJwtLoader(c.jwtPath)
	if tlErr != nil {
		return nil, fmt.Errorf("failed to create token loader to load the tokens: %v", tlErr)
	}
	stopCh := make(chan struct{})
	go jwtLoader.Run(stopCh)
	c.jwtLoader = jwtLoader

	var client *vaultapi.Client
	var err error
	if c.enableTLS {
		client, err = createVaultTLSClient(c.vaultAddr, c.tlsRootCertPath)
	} else {
		client, err = createVaultClient(c.vaultAddr)
	}
	if err != nil {
		return nil, err
	}
	c.client = client

	token, err := loginVaultK8sAuthMethod(c.client, c.loginPath, c.loginRole, jwtLoader.Jwt)
	if err != nil {
		return nil, fmt.Errorf("failed to login Vault at %s: %v", c.vaultAddr, err)
	}
	c.client.SetToken(token)

	vaultClientLog.Infof("created Vault client for Vault address: %s, TLS: %v", c.vaultAddr, c.enableTLS)
	return c, nil
}

// CSRSign calls Vault to sign a CSR. It returns a PEM-encoded cert chain or error.
func (c *VaultClient) CSRSign(ctx context.Context, reqID string, csrPEM []byte, jwt string,
	certValidTTLInSec int64) ([]string, error) {
	if len(jwt) != 0 {
		token, err := loginVaultK8sAuthMethod(c.client, c.loginPath, c.loginRole, jwt)
		if err != nil {
			return nil, fmt.Errorf("failed to login Vault at %s: %v", c.vaultAddr, err)
		}
		c.client.SetToken(token)
	}
	certChain, err := signCsrByVault(c.client, c.signCsrPath, certValidTTLInSec, csrPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to sign CSR: %v", err)
	}

	if len(certChain) <= 1 {
		vaultClientLog.Errorf("certificate chain length is %d, expected more than 1", len(certChain))
		return nil, fmt.Errorf("invalid certificate chain in the response")
	}

	return certChain, nil
}

// GetCACertPem returns the CA certificate in PEM format.
func (c *VaultClient) GetCACertPem() (string, error) {
	resp, err := c.client.Logical().Read(c.caCertPath)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve CA cert: %v", err)
	}
	if resp == nil || resp.Data == nil {
		return "", fmt.Errorf("failed to retrieve CA cert: Got nil data [%v]", resp)
	}
	certData, ok := resp.Data[certKeyInCACertResp]
	if !ok {
		return "", fmt.Errorf("no certificate in the CA cert response [%v]", resp.Data)
	}
	cert, ok := certData.(string)
	if !ok {
		return "", fmt.Errorf("the certificate in the CA cert response is not a string")
	}
	return cert, nil
}

// createVaultClient creates a client to a Vault server
// vaultAddr: the address of the Vault server (e.g., "http://127.0.0.1:8200").
func createVaultClient(vaultAddr string) (*vaultapi.Client, error) {
	config := vaultapi.DefaultConfig()
	config.Address = vaultAddr

	client, err := vaultapi.NewClient(config)
	if err != nil {
		vaultClientLog.Errorf("failed to create a Vault client: %v", err)
		return nil, err
	}

	return client, nil
}

// createVaultTLSClient creates a client to a Vault server
// vaultAddr: the address of the Vault server (e.g., "https://127.0.0.1:8200").
func createVaultTLSClient(vaultAddr string, tlsRootCertPath string) (*vaultapi.Client, error) {
	// Load the system default root certificates.
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("could not get SystemCertPool: %v", err)
	}
	if pool == nil {
		log.Info("system cert pool is nil, create a new cert pool")
		pool = x509.NewCertPool()
	}
	tlsRootCert, err := ioutil.ReadFile(tlsRootCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS cert from file [%s]: %v", tlsRootCertPath, err)
	}
	if len(tlsRootCert) > 0 {
		ok := pool.AppendCertsFromPEM(tlsRootCert)
		if !ok {
			return nil, fmt.Errorf("failed to append certificate [%v] to the certificate pool", string(tlsRootCert))
		}
	}
	tlsConfig := &tls.Config{
		RootCAs: pool,
	}

	transport := &http.Transport{TLSClientConfig: tlsConfig}
	httpClient := &http.Client{Transport: transport}

	config := vaultapi.DefaultConfig()
	config.Address = vaultAddr
	config.HttpClient = httpClient

	client, err := vaultapi.NewClient(config)
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
func loginVaultK8sAuthMethod(client *vaultapi.Client, loginPath, role, jwt string) (string, error) {
	resp, err := client.Logical().Write(
		loginPath,
		map[string]interface{}{
			jwtKeyInLoginReq:  jwt,
			roleKeyInLoginReq: role,
		})

	if err != nil {
		vaultClientLog.Errorf("failed to login Vault: %v", err)
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("login response is nil")
	}
	if resp.Auth == nil {
		return "", fmt.Errorf("login response auth field is nil")
	}
	return resp.Auth.ClientToken, nil
}

// signCsrByVault signs the CSR and return the signed certificate and the CA certificate chain
// Return the signed certificate chain when succeed.
// client: the Vault client
// csrSigningPath: the path for signing a CSR
// csr: the CSR to be signed, in pem format
func signCsrByVault(client *vaultapi.Client, csrSigningPath string, certTTLInSec int64, csr []byte) ([]string, error) {
	m := map[string]interface{}{
		"format":               "pem",
		"csr":                  string(csr),
		"ttl":                  strconv.FormatInt(certTTLInSec, 10) + "s",
		"exclude_cn_from_sans": true,
	}
	resp, err := client.Logical().Write(csrSigningPath, m)
	if err != nil {
		return nil, fmt.Errorf("failed to post to %v: %v", csrSigningPath, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("sign response is nil")
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("sign response has a nil Data field")
	}
	//Extract the certificate and the certificate chain
	certificateData, certOK := resp.Data[certKeyInCertSignResp]
	if !certOK {
		return nil, fmt.Errorf("no certificate in the CSR response [%v]", resp.Data)
	}
	cert, ok := certificateData.(string)
	if !ok {
		return nil, fmt.Errorf("the certificate in the CSR response is not a string")
	}
	var certChain []string
	certChain = append(certChain, cert+"\n")

	caChainData, caChainOK := resp.Data[caChainKeyInCertSignResp]
	if caChainOK {
		chain, ok := caChainData.([]interface{})
		if !ok {
			return nil, fmt.Errorf("the certificate chain in the CSR response is of unexpected format")
		}
		for idx, c := range chain {
			cert, ok := c.(string)
			if !ok {
				return nil, fmt.Errorf("the certificate in the certificate chain position %v is not a string", idx)
			}
			certChain = append(certChain, cert+"\n")
		}
	} else {
		issuingCAData, issuingCAOK := resp.Data[issuingCAKeyInCertSignResp]
		if !issuingCAOK {
			return nil, fmt.Errorf("no cert chain or issuing CA in the CSR response")
		}
		issuingCA, ok := issuingCAData.(string)
		if !ok {
			return nil, fmt.Errorf("the issuing CA cert in the CSR response is not a string")
		}
		certChain = append(certChain, issuingCA+"\n")
	}

	return certChain, nil
}
