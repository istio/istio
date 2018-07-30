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

package ca

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"istio.io/istio/pkg/log"
	capb "istio.io/istio/security/proto/ca/v1alpha1"
)

// Client interface defines the clients need to implement to talk to CA for CSR.
type Client interface {
	CSRSign(ctx context.Context, csrPEM []byte, subjectID string,
		certValidTTLInSec int64) ([]byte /*PEM-encoded certificate chain*/, error)
}

type caClient struct {
	client capb.IstioCertificateServiceClient
}

// NewCAClient create an CA client.
func NewCAClient(endpoint, rootPath string) (Client, error) {
	var opts grpc.DialOption
	if rootPath != "" {
		creds, err := credentials.NewClientTLSFromFile(rootPath, "")
		if err != nil {
			log.Errorf("Failed to read root certificate file: %v", err)
			return nil, fmt.Errorf("failed to read root cert")
		}
		opts = grpc.WithTransportCredentials(creds)
	} else {
		opts = grpc.WithInsecure()
	}

	conn, err := grpc.Dial(endpoint, opts)
	if err != nil {
		log.Errorf("Failed to connect to endpoint %q: %v", endpoint, err)
		return nil, fmt.Errorf("failed to connect to endpoint %q", endpoint)
	}

	return &caClient{
		client: capb.NewIstioCertificateServiceClient(conn),
	}, nil
}

func (cl *caClient) CSRSign(ctx context.Context, csrPEM []byte, token string,
	certValidTTLInSec int64) ([]byte /*PEM-encoded certificate chain*/, error) {
	// TODO(quanlin): remove SubjectId field once CA put it as optional.
	subjectID, err := parseSubjectID(token)
	if err != nil {
		return nil, errors.New("failed to construct IstioCertificate request")
	}

	req := &capb.IstioCertificateRequest{
		Csr:              string(csrPEM),
		SubjectId:        subjectID,
		ValidityDuration: certValidTTLInSec,
	}

	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", token))
	resp, err := cl.client.CreateCertificate(ctx, req)
	if err != nil {
		log.Errorf("Failed to create certificate: %v", err)
		return nil, err
	}

	if len(resp.CertChain) <= 1 {
		log.Errorf("CertChain length is %d, expected more than 1", len(resp.CertChain))
		return nil, errors.New("invalid response cert chain")
	}

	// Returns the leaf cert(Leaf cert is element '0', Root cert is element 'n').
	ret := []byte{}
	for _, c := range resp.CertChain {
		ret = append(ret, []byte(c)...)
	}

	return ret, nil
}

// TODO(quanlin): remove below workaround function once CA put SubjectId field as optional.
// token format - "Bearer ya29.****"
type content struct {
	ID string `json:"azp"`
}

const tokenPrefix = "Bearer "

// Parse subjectID from token by sending web request and parsing the response content.
// This function can be removed once CA put SubjectId field as optional.
func parseSubjectID(token string) (string, error) {
	if !strings.HasPrefix(token, tokenPrefix) {
		log.Errorf("invalid token %q, should start with %q", token, tokenPrefix)
		return "", errors.New("invalid token")
	}

	strs := strings.Fields(token)
	url := fmt.Sprintf("https://www.googleapis.com/oauth2/v3/tokeninfo?access_token=%s", strs[1])
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		log.Errorf("Request against %q failed, %v", url, err)
		return "", errors.New("failed to parse subject ID")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Failed to read response from %q: %v", url, err)
		return "", errors.New("failed to parse subject ID")
	}

	item := content{}
	if err := json.Unmarshal(body, &item); err != nil {
		log.Errorf("failed to parse json %v", err)
		return "", errors.New("failed to parse subject ID")
	}

	return item.ID, nil
}
