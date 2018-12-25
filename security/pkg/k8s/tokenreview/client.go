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
	"strings"

	"net/http"

	"istio.io/istio/pkg/log"
	k8sauth "k8s.io/api/authentication/v1"
)

type specForJWTValidationRequest struct {
	Token string `json:"token"`
}

type jwtValidationRequest struct {
	APIVersion string                      `json:"apiVersion"`
	Kind       string                      `json:"kind"`
	Spec       specForJWTValidationRequest `json:"spec"`
}

type tokenReviewClient struct {
	url         string
	caCert      []byte
	reviewerJWT string
}

// NewClient creates a new tokenReviewClient.
func NewClient(url string, caCert []byte, reviewerJWT string) *tokenReviewClient {
	return &tokenReviewClient{
		url:         url,
		caCert:      caCert,
		reviewerJWT: reviewerJWT,
	}
}

// Review calls the K8s TokenReview API to verify a JWT, and returns the
// <namespace>:<serviceaccountname> in the JWT if successful.
// targetJWT: the target JWT to be verified.
func (c *tokenReviewClient) Review(targetJWT string) (string, error) {
	resp, err := c.reviewJWTAtK8sAPIServer(targetJWT)
	if err != nil {
		return "", fmt.Errorf("failed to get a token review response: %v", err)
	}
	// Check that the JWT is valid
	if !(resp.StatusCode == http.StatusOK ||
		resp.StatusCode == http.StatusCreated ||
		resp.StatusCode == http.StatusAccepted) {
		return "", fmt.Errorf("invalid review response status code %v", resp.StatusCode)
	}
	defer resp.Body.Close()
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read from the response body: %v", err)
	}
	log.Infof("Response body is: %v", string(bodyBytes))
	tokenReview := &k8sauth.TokenReview{}
	err = json.Unmarshal(bodyBytes, tokenReview)
	if err != nil {
		return "", fmt.Errorf("unmarshal response body returns an error: %v", err)
	}
	if tokenReview.Status.Error != "" {
		return "", fmt.Errorf("the service account authentication returns an error: %v" + tokenReview.Status.Error)
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
		return "", fmt.Errorf("the JWT is not authenticated")
	}
	inServiceAccountGroup := false
	for _, group := range tokenReview.Status.User.Groups {
		if group == "system:serviceaccounts" {
			inServiceAccountGroup = true
			break
		}
	}
	if !inServiceAccountGroup {
		return "", fmt.Errorf("the JWT is not a service account")
	}
	// "username" is in the form of system:serviceaccount:{namespace}:{service account name}",
	// e.g., "username":"system:serviceaccount:default:example-pod-sa"
	subStrings := strings.Split(tokenReview.Status.User.Username, ":")
	if len(subStrings) != 4 {
		return "", fmt.Errorf("invalid username field in the token review result")
	}

	return subStrings[2] + ":" + subStrings[3], nil
}

// reviewJWTAtK8sAPIServer reviews the target JWT at k8s API server.
// targetJWT: the target JWT to be verified.
func (c *tokenReviewClient) reviewJWTAtK8sAPIServer(targetJWT string) (*http.Response, error) {
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(c.caCert)
	req := jwtValidationRequest{
		APIVersion: "authentication.k8s.io/v1",
		Kind:       "TokenReview",
		Spec:       specForJWTValidationRequest{Token: targetJWT},
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the service account review request: %v", err)
	}
	httpReq, err := http.NewRequest("POST", c.url, bytes.NewBuffer(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create an HTTP request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.reviewerJWT)
	// Set the TLS certificate
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		},
	}
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send the HTTP request: %v", err)
	}
	return httpResp, nil
}
