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
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	vaultapi "github.com/hashicorp/vault/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const (
	// The environmental variable name for Vault CA address.
	envVaultAddr = "VAULT_ADDR"
	// The environmental variable name for the configmap name storing the TLS cert to authenticate Vault.
	envVaultTLSCertCM = "VAULT_AUTH_CERT_CM"
	// The environmental variable name for the path to the K8s JWT to authenticate to Vault.3
	envJwtPath = "VAULT_AUTH_JWT_PATH"
	// The environmental variable name for Vault auth path.
	envLoginPath = "VAULT_AUTH_PATH"
	// The environmental variable name for Vault role.
	envLoginRole = "VAULT_ROLE"
	// The environmental variable name for Vault KV storage path.
	envVaultKVPath = "VAULT_KV_PATH"

	// The key of root cert in the configmap.
	tlsRootCertKeyInCm = "cert"

	// The key for PIN in Vault KV store.
	pinKey = "PIN"
	// The key for client cert PEM in Vault KV store.
	clientCertKey = "clientcert"
	// The key for client cert PEM in Vault KV store.
	clientKeyKey = "clientkey"
	// The key for server cert PEM in Vault KV store.
	serverCertKey = "servercert"

	jwtKeyInLoginReq  = "jwt"
	roleKeyInLoginReq = "role"
)

var vaultClientLog = log.RegisterScope("vault", "Vault client debugging", 0)

// Client is a client for interaction with Vault.
type Client struct {
	vaultAddr     string
	tlsRootCertCM string
	loginRole     string
	loginPath     string
	kvPath        string

	client *vaultapi.Client
	jwt    string
}

// NewVaultClient creates a CA client for the Vault PKI.
func NewVaultClient(cmGetter corev1.ConfigMapsGetter) (*Client, error) {
	vaultAddr := env.RegisterStringVar(envVaultAddr, "", "The address of the Vault server").Get()
	if len(vaultAddr) == 0 {
		return nil, fmt.Errorf("%s is not configured", envVaultAddr)
	}
	vaultClientLog.Infof("%s = %s", envVaultAddr, vaultAddr)

	tlsRootCertCM := env.RegisterStringVar(envVaultTLSCertCM, "", "The name of the config map storing the TLS "+
		"cert to authenticate the Vault server. Key should be 'cert'. If not stored in istio-system, use the "+
		"format <name>.<namespace>").Get()
	vaultClientLog.Infof("%s = %s", envVaultTLSCertCM, tlsRootCertCM)

	jwtPath := env.RegisterStringVar(envJwtPath, "", "The JWT path to get authenticated by the Vault server").Get()
	if len(jwtPath) == 0 {
		return nil, fmt.Errorf("%s is not configured", envJwtPath)
	}
	vaultClientLog.Infof("%s = %s", envJwtPath, jwtPath)

	loginRole := env.RegisterStringVar(envLoginRole, "", "The login role for the Vault server").Get()
	if len(loginRole) == 0 {
		return nil, fmt.Errorf("%s is not configured", envLoginRole)
	}
	vaultClientLog.Infof("%s = %s", envLoginRole, loginRole)

	loginPath := env.RegisterStringVar(envLoginPath, "", "The login path on the Vault server").Get()
	if len(loginPath) == 0 {
		return nil, fmt.Errorf("%s is not configured", envLoginPath)
	}
	vaultClientLog.Infof("%s = %s", envLoginPath, loginPath)

	vaultKVPath := env.RegisterStringVar(envVaultKVPath, "", "The path to the key-value store in Vault").Get()
	if len(vaultKVPath) == 0 {
		return nil, fmt.Errorf("%s is not configured", envVaultKVPath)
	}
	vaultClientLog.Infof("%s = %s", envVaultKVPath, vaultKVPath)

	c := &Client{
		vaultAddr:     vaultAddr,
		tlsRootCertCM: tlsRootCertCM,
		jwt:           "",
		loginRole:     loginRole,
		loginPath:     loginPath,
		kvPath:        vaultKVPath,
	}

	tokenBytes, rErr := ioutil.ReadFile(jwtPath)
	if rErr != nil || len(tokenBytes) == 0 {
		return nil, fmt.Errorf("failed to read JWT [%s]: %v", jwtPath, rErr)
	}
	c.jwt = string(tokenBytes)

	var client *vaultapi.Client
	var err error

	if len(tlsRootCertCM) == 0 {
		return nil, fmt.Errorf("%s is not configured", envVaultTLSCertCM)
	}
	segments := strings.Split(tlsRootCertCM, ".")
	var name string
	namespace := "istio-system"
	if len(segments) > 2 {
		return nil, fmt.Errorf("incorrect format of the TLS CA cert configmap name: %s", tlsRootCertCM)
	}
	name = segments[0]
	if len(segments) == 2 {
		namespace = segments[1]
	}
	client, err = createVaultTLSClient(c.vaultAddr, name, namespace, cmGetter)
	if err != nil {
		return nil, err
	}
	c.client = client

	token, err := loginVaultK8sAuthMethod(c.client, c.loginPath, c.loginRole, c.jwt)
	if err != nil {
		return nil, fmt.Errorf("failed to login Vault at %s: %v", c.vaultAddr, err)
	}
	c.client.SetToken(token)
	vaultClientLog.Infof("created secure Vault client for Vault address: %s", c.vaultAddr)

	return c, nil
}

// createVaultTLSClient creates a client to a Vault server
// vaultAddr: the address of the Vault server (e.g., "https://127.0.0.1:8200").
func createVaultTLSClient(vaultAddr, cmName, cmNamespace string, cmGetter corev1.ConfigMapsGetter) (
	*vaultapi.Client, error) {
	// Load the system default root certificates.
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("could not get SystemCertPool: %v", err)
	}
	if pool == nil {
		log.Info("system cert pool is nil, create a new cert pool")
		pool = x509.NewCertPool()
	}

	// Read cert from K8s ConfigMap
	configmap, err := cmGetter.ConfigMaps(cmNamespace).Get(context.TODO(), cmName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS root cert ConfigMap %s in %s to authenticate Vault server: %v", cmName, cmNamespace, err)
	}
	tlsRootCert := configmap.Data[tlsRootCertKeyInCm]
	if len(tlsRootCert) == 0 {
		return nil, fmt.Errorf("the TLS root cert in ConfigMap %s.%s does not contain the key: %s",
			cmName, cmNamespace, tlsRootCertKeyInCm)
	}

	ok := pool.AppendCertsFromPEM([]byte(tlsRootCert))
	if !ok {
		return nil, fmt.Errorf("failed to append certificate [%s] to the certificate pool", tlsRootCert)
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
		vaultClientLog.Errorf("failed to log into Vault: %v", err)
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

// GetHSMCredentilals retrieves all the credentials for HSM.
func (c *Client) GetHSMCredentilals() (clientCertPem, clientKeyPem, serverCertPem, pin []byte, err error) {
	resp, err := c.client.Logical().Read(c.kvPath)
	if err != nil {
		err = fmt.Errorf("failed to access KV endpoint: %v", err)
		return
	}
	if resp == nil || resp.Data == nil {
		err = fmt.Errorf("failed to retrieve CA cert: Got nil data: %v", resp)
		return
	}

	if clientCertPem, err = getValue(resp, clientCertKey); err != nil {
		return
	}
	if clientKeyPem, err = getValue(resp, clientKeyKey); err != nil {
		return
	}
	if serverCertPem, err = getValue(resp, serverCertKey); err != nil {
		return
	}
	if pin, err = getValue(resp, pinKey); err != nil {
		return
	}
	return
}

func getValue(resp *vaultapi.Secret, key string) ([]byte, error) {
	data, ok := resp.Data[key]
	if !ok {
		return []byte{}, fmt.Errorf("no value for %s in Vault", key)
	}
	strData, ok := data.(string)
	if !ok {
		return []byte{}, fmt.Errorf("value of %s in Vault is not a string", key)
	}
	return []byte(strData), nil
}
