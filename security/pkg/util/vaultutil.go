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

package util

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"

	"github.com/hashicorp/vault/api"
	k8sauth "k8s.io/api/authentication/v1"

	pkiutil "istio.io/istio/security/pkg/pki/util"
)

type specForSaValidationRequest struct {
	Token string `json:"token"`
}

type saValidationRequest struct {
	APIVersion string                     `json:"apiVersion"`
	Kind       string                     `json:"kind"`
	Spec       specForSaValidationRequest `json:"spec"`
}

// CreateVaultClient creates a client to a Vault server
// vaultAddr: the address of the Vault server (e.g., "http://127.0.0.1:8200").
func CreateVaultClient(vaultAddr string) (*api.Client, error) {
	config := api.DefaultConfig()
	config.Address = vaultAddr

	client, err := api.NewClient(config)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// LoginVaultK8sAuthMethod logs into the Vault k8s auth method with the service account and
// returns the auth client token.
// loginPath: the path of the login
// role: the login role
// jwt: the service account used for login
func LoginVaultK8sAuthMethod(client *api.Client, loginPath, role, sa string) (string, error) {
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

// SignCsrByVault signs the CSR and return the signed certifcate and the CA certificate chain
// Return the signed certificate and the CA certificate chain when succeed.
// client: the Vault client
// csrSigningPath: the path for signing a CSR
// csr: the CSR to be signed, in pem format
func SignCsrByVault(client *api.Client, csrSigningPath string, csr []byte) ([]byte, []byte, error) {
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
	certChain := ""
	for i, c := range chain {
		_, ok := c.(string)
		if !ok {
			return nil, nil, fmt.Errorf("the certificate in the certificate chain is not a string")
		}
		certChain += c.(string)
		if i < len(chain) {
			certChain += "\n"
		}
	}

	return []byte(cert), []byte(certChain), nil
}

// ReviewServiceAccountAtK8sAPIServer reviews the CSR credential (k8s service account) at k8s API server.
// k8sAPIServerURL: the URL of k8s API Server
// k8sAPIServerCaCert: the CA certificate of k8s API Server
// reviewerToken: the service account of the k8s token reviewer
// csrCredential: the credential of the CSR requester
func ReviewServiceAccountAtK8sAPIServer(k8sAPIServerURL string, k8sAPIServerCaCert []byte,
	reviewerToken string, csrCredential []byte) (*http.Response, error) {
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(k8sAPIServerCaCert)
	saReq := saValidationRequest{
		APIVersion: "authentication.k8s.io/v1",
		Kind:       "TokenReview",
		Spec:       specForSaValidationRequest{Token: string(csrCredential[:])},
	}
	saReqJSON, err := json.Marshal(saReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the service account review request: %v", err)
	}
	req, err := http.NewRequest("POST", k8sAPIServerURL, bytes.NewBuffer(saReqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create a HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+reviewerToken)
	// Set the TLS certificate
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		},
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send the HTTP request: %v", err)
	}
	return resp, nil
}

// ValidateCsrRequest validates that the identity in CSR request matches that in the requester's
// credential.
// Return nil when the validation passes. Otherwise, return the error.
// k8sAPIServerURL: the URL of k8s API Server
// k8sAPIServerCaCert: the CA certificate of k8s API Server
// reviewerToken: the service account of the k8s token reviewer
// csr: CSR from the requester
// csrCredential: the credential of the CSR requester
func ValidateCsrRequest(k8sAPIServerURL string, k8sAPIServerCaCert []byte, reviewerToken string,
	csr string, csrCredential []byte) error {
	resp, err := ReviewServiceAccountAtK8sAPIServer(k8sAPIServerURL, k8sAPIServerCaCert,
		reviewerToken, csrCredential)
	if err != nil {
		return fmt.Errorf("failed to get a token review response: %v", err)
	}
	// Check that the SA is valid
	if !(resp.StatusCode == http.StatusOK ||
		resp.StatusCode == http.StatusCreated ||
		resp.StatusCode == http.StatusAccepted) {
		return fmt.Errorf("invalid review response status code %v", resp.StatusCode)
	}
	defer resp.Body.Close()
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read from the response body: %v", err)
	}
	//log.Infof("Response body is: %v", string(bodyBytes))
	tokenReview := &k8sauth.TokenReview{}
	err = json.Unmarshal(bodyBytes, tokenReview)
	if err != nil {
		return fmt.Errorf("unmarshal response body returns an error: %v", err)
	}
	if tokenReview.Status.Error != "" {
		return fmt.Errorf("the service account authentication returns an error: %v" + tokenReview.Status.Error)
	}
	// An example SA token:
	// {"alg":"RS256","typ":"JWT"}
	// {"iss":"kubernetes/serviceaccount",
	//  "kubernetes.io/serviceaccount/namespace":"default",
	//  "kubernetes.io/serviceaccount/secret.name":"example-pod-sa-token-h4jqx",
	//  "kubernetes.io/serviceaccount/service-account.name":"example-pod-sa",
	//  "kubernetes.io/serviceaccount/service-account.uid":"ff578a9e-65d3-11e8-aad2-42010a8a001d",
	//  "sub":"system:serviceaccount:default:example-pod-sa"
	//  }

	// An example token review status
	// "status":{
	//   "authenticated":true,
	//   "user":{
	//     "username":"system:serviceaccount:default:example-pod-sa",
	//     "uid":"ff578a9e-65d3-11e8-aad2-42010a8a001d",
	//     "groups":["system:serviceaccounts","system:serviceaccounts:default","system:authenticated"]
	//    }
	// }

	if !tokenReview.Status.Authenticated {
		return fmt.Errorf("the token is not authenticated")
	}
	inServiceAccountGroup := false
	for _, group := range tokenReview.Status.User.Groups {
		if group == "system:serviceaccounts" {
			inServiceAccountGroup = true
			break
		}
	}
	if !inServiceAccountGroup {
		return fmt.Errorf("the token is not a service account")
	}
	// "username" is in the form of system:serviceaccount:{namespace}:{service account name}",
	// e.g., "username":"system:serviceaccount:default:example-pod-sa"
	subStrings := strings.Split(tokenReview.Status.User.Username, ":")
	if len(subStrings) != 4 {
		return fmt.Errorf("invalid username field in the token review result")
	}
	namespace := subStrings[2]
	saName := subStrings[3]

	err = ValidateCsrSpiffeMatchServiceAccount(csr, namespace, saName)
	if err != nil {
		return fmt.Errorf("failed to validate the SPIFFE id in CSR: %v", err)
	}
	return nil
}

// ValidateCsrSpiffeMatchServiceAccount validates that the SPIFFE identity in CSR matches the
// namespace and service account name in the service account.
// Return nil when matching. Otherwise, return the error.
// csrPem: the CSR to validate.
// namespace: the service account namespace.
// saName: the service account name.
func ValidateCsrSpiffeMatchServiceAccount(csrPem string, namespace string, saName string) error {
	csr, err := pkiutil.ParsePemEncodedCSR([]byte(csrPem))
	if err != nil {
		return fmt.Errorf("failed to parse CSR: %v", err)
	}

	ids, err := pkiutil.ExtractIDs(csr.Extensions)
	if err != nil {
		return fmt.Errorf("failed to extract SPIFFE IDs from CSR: %v", err)
	}
	// Regular expression to parse a spiffe id,
	// e.g., "spiffe://cluster.local/ns/example_namespace/sa/example_pod_sa"
	rx, err := regexp.Compile(`:*/+`)
	if err != nil {
		return fmt.Errorf("failed to create the regular expression: %v", err)
	}
	for _, id := range ids {
		substrs := rx.Split(id, -1)
		if len(substrs) == 6 && strings.EqualFold(substrs[0], "spiffe") &&
			namespace == substrs[3] && saName == substrs[5] {
			// CSR contains a SPIFFE identity matching the identity in the service account
			return nil
		}
	}
	return fmt.Errorf("the SPIFFE identity in CSR does not match the identity in the service account")
}
