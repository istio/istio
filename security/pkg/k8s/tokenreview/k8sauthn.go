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

package tokenreview

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	k8sauth "k8s.io/api/authentication/v1"
)

type specForSaValidationRequest struct {
	Token string `json:"token"`
}

type saValidationRequest struct {
	APIVersion string                     `json:"apiVersion"`
	Kind       string                     `json:"kind"`
	Spec       specForSaValidationRequest `json:"spec"`
}

// K8sSvcAcctAuthn authenticates a k8s service account (JWT) through the k8s TokenReview API.
type K8sSvcAcctAuthn struct {
	apiServerAddr string
	callerToken   string
	httpClient    *http.Client
}

// NewK8sSvcAcctAuthn creates a new authenticator for authenticating k8s JWTs.
// It creates an HTTP client singleton to talk to the apiserver TokenReview API.
// apiServerAddr: the URL of k8s API Server
// apiServerCert: the CA certificate of k8s API Server
// callerToken: the JWT of the caller to authenticate to k8s API server
func NewK8sSvcAcctAuthn(apiServerAddr string, apiServerCert []byte, callerToken string) *K8sSvcAcctAuthn {
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(apiServerCert)
	// Set the TLS certificate
	// Create the HTTP client so that for one instance, only one connection is used to serve all requests.
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
			// Bump up the number of connections (default to 2) kept in the pool for
			// re-use. This can greatly improve the connection re-use with heavy
			// traffic.
			MaxIdleConnsPerHost: 100,
		},
	}

	return &K8sSvcAcctAuthn{
		apiServerAddr: apiServerAddr,
		callerToken:   callerToken,
		httpClient:    httpClient,
	}
}

// reviewServiceAccountAtK8sAPIServer reviews the CSR credential (k8s service account) at k8s API server.
// targetToken: the JWT of the K8s service account to be reviewed
func (authn *K8sSvcAcctAuthn) reviewServiceAccountAtK8sAPIServer(targetToken string) (*http.Response, error) {
	saReq := saValidationRequest{
		APIVersion: "authentication.k8s.io/v1",
		Kind:       "TokenReview",
		Spec:       specForSaValidationRequest{Token: targetToken},
	}
	saReqJSON, err := json.Marshal(saReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the service account review request: %v", err)
	}
	req, err := http.NewRequest("POST", authn.apiServerAddr, bytes.NewBuffer(saReqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create a HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authn.callerToken)
	resp, err := authn.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send the HTTP request: %v", err)
	}
	return resp, nil
}

// ValidateK8sJwt validates a k8s JWT at API server.
// Return {<namespace>, <serviceaccountname>} in the targetToken when the validation passes.
// Otherwise, return the error.
// targetToken: the JWT of the K8s service account to be reviewed
func (authn *K8sSvcAcctAuthn) ValidateK8sJwt(targetToken string) ([]string, error) {
	resp, err := authn.reviewServiceAccountAtK8sAPIServer(targetToken)
	if err != nil {
		return nil, fmt.Errorf("failed to get a token review response: %v", err)
	}
	defer resp.Body.Close()
	// Check that the JWT is valid
	if !(resp.StatusCode == http.StatusOK ||
		resp.StatusCode == http.StatusCreated ||
		resp.StatusCode == http.StatusAccepted) {
		return nil, fmt.Errorf("invalid review response status code %v", resp.StatusCode)
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read from the response body: %v", err)
	}
	tokenReview := &k8sauth.TokenReview{}
	err = json.Unmarshal(bodyBytes, tokenReview)
	if err != nil {
		return nil, fmt.Errorf("unmarshal response body returns an error: %v", err)
	}
	if tokenReview.Status.Error != "" {
		return nil, fmt.Errorf("the service account authentication returns an error: %v" + tokenReview.Status.Error)
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
		return nil, fmt.Errorf("the token is not authenticated")
	}
	inServiceAccountGroup := false
	for _, group := range tokenReview.Status.User.Groups {
		if group == "system:serviceaccounts" {
			inServiceAccountGroup = true
			break
		}
	}
	if !inServiceAccountGroup {
		return nil, fmt.Errorf("the token is not a service account")
	}
	// "username" is in the form of system:serviceaccount:{namespace}:{service account name}",
	// e.g., "username":"system:serviceaccount:default:example-pod-sa"
	subStrings := strings.Split(tokenReview.Status.User.Username, ":")
	if len(subStrings) != 4 {
		return nil, fmt.Errorf("invalid username field in the token review result")
	}
	namespace := subStrings[2]
	saName := subStrings[3]

	return []string{namespace, saName}, nil
}
