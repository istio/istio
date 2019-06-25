// Copyright 2019 Istio Authors
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

package spire

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	spireNode "github.com/spiffe/spire/proto/spire/api/node"
	spireCommon "github.com/spiffe/spire/proto/spire/common"
)

const (
	mockServerAddress          = "localhost:0"
	defaultCertificateSpiffeID = "spiffe://example.org/ns/istio-system/sa/istio-ingressgateway-service-account"
	defaultTrustDomain         = "spiffe://example.org"
	fakeToken                  = "Bearer fakeToken"
	serverKey                  = `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQC9mnns9H8QIkk/
GO1fMCnsTKj3jLxH0V7FNSSnTK760AIr8DT1zep0T5U1T7/LcNAErsU0sKhYABz7
fTvXWJEfrTSaIXQGh094KX7SBadz5Rw38lOvcAknMT2Eu/e1ASU/0giEFUT7fUEW
0X43dzLE2MUQ4NRb2FyrB2vnp8teZfAVTfsMbQoFYQfzKE3/e5Kat+zuW00NvuQy
4unc+YQ7jXtPDAsEArXCEhIyOxSDaCGSiRSbfgi5WuT1htXIHgFmnB7MPgFFdlwM
Rad+zLVb/3TGfFU2NA9elArjWfXO1RfCUKY/Yqkhp+O2rY74wVs8F6Ju7P81/fU4
2Y6l4qPhAgMBAAECggEAQXG5lgWKeioreCEFhe6c+dg4FkI4lt14xb8jKK/6Uc5M
gZgG37U0sPLrQJyHShRlaMhef2JeqQlY96Fxb1I9vV5OosjbQImh74r7IEkdVI6H
X/Q/2HVmY2XGozMyPALqNY4srFKfHeNM/TBQTZrSJkngM4Q3KICU89+66hnrw2f5
ONi3RiD3NKt9o0SXC9iOIetU2hWsgL8rM4mAAC0Bv2zRb+q3OqA1cfOYc8THBTRP
y7lELKqw1jEaKzTynrLmM0qyBLnnp1pAl+nzFSnJFuf+qZ41qCo9uWT52kpGgop/
fhxySVq4c/26W2fTEUGmO4ZAZchqZa/dbr/Xb9U5AQKBgQD0u9FErbKB9/RPujg5
lvGz6eoIHduRfMm6DAZ0AmLkp+MgOgzAZqqrk8MFcS9Nr0/LQ9Ci4fFRO3SOZCdd
+bvjxh0LO3QQH5d3Y2lxv5KgAETuCzHeLe2CK2stTkzl9/K9muxxZFwNzDbz65A2
k9p/TA1IacbkOWsVRDMtJ0UiUQKBgQDGVPMkfykDxeqgz3kOXxrRNPJZIuceDXhZ
2Dkpl8MLCZHsk0fu4DXrIB3uCS1snDg9UcCyBz3pT2kZ3yoCQuhelXmSqT7dNGFJ
jzLXS3HrEgsP9wXkhO6+rnjflRJ8JPWixIQmTeR3P1NrESz1EIx3IsqU6bgARwwi
XiG+lHX0kQKBgBMvzl5GB+Ksn9jITrQlI1npktGEFby4PdB6NN9PeJVYnDPgmTNU
WTkOYpHAp+a9QdI7xNWgRR0LPj4TmAqEE7jtxUUmKhlBgMx5XMDwNfyZSM4ozoYO
r7ou0T5CD0FQSRWYWcUiCx2BzyUcaLf+q3ija78rm840ujJ2oFR/6amhAoGBAIaC
1uTJ2WdVs+ucyt2UYvvAjR0nLtiTCizlGN+8reuucemhegfoyKjO/32Re91NllcA
O1CC2NqDoSSK1lLyTebYObveTWR5QgJBvJmH8ZscgaQyRSzXe5SXgCMjV4YbCv15
iqbv7SNzL4BOBc+viZTDY+HbIZAOn4wvi3NV/SoBAoGBALyj3FPyFzZD5u90KXUM
z/NWpewYAn4sPNHkFdRNndJ5xWE+P2gpv08/zSYQ5zaPHoIGvaZoWorrylrx8obL
2jdEQWKmg+qSGeHqiPRZ6LKKLMw4EIs7RCTklZlsUEJ+fkfwz/3KIGRdBxPsQkxs
a3CMN9/boXf+OKBcL96yIHOD
-----END PRIVATE KEY-----
`
	alternativeKey = `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCjEtxK5IkZHfkK
OowAuAY++fD75l/3U8LsTJdPQ+cqE2ZkzoFF2HE8xais//eDmguccnKBgIPzaT+K
FSCSjozbBO4UTBd0NL/21O69gMrDHbSnOK5sz67Hrb37OIOuHC4Vm5th7dPQaZwD
ZTvqVKit/WB3VlxbYXmw+3ku3mFLTKrfYtsNF2uEksJJ0tMo3q3EHT9mHnLC0HV9
5nKcZiiBgB2Z1CtdgaJRr/bysFe40/AMSeSB4F4xxKl00b2q+AnTLCXaQBEpN6Sv
pCWUkDXyzx/jS/PT5Z8Rk7GUnQpUimLDKhktYNWz7I+xtuqc5YNMaJlXD+DVoYFQ
S/HqcorbAgMBAAECggEALJKGJe5LTtMzc8lG5RdnlaUJakCwsFBzsdTJcr/zmjuN
PDZ5fRbI9Lxt+0NHavAbBlr900nGRyzYUiyuJ4DRHTg+vsuBiaC1a4kN7Dwcr7IZ
468JdfJaKnfhup3a2CcZrYxHrz+rKocDPqZX9xfGty/PQy8WtV9yPJ6vo9DitQrC
vPdpSMF1GNwPmIWnteHapm2o9BKbQhCJK3VG3rlkB36CKjD799leNAGjFwraFvBi
W/uvUD14Y9HPUSL6SG4mhZ9pQLRsry1B9DoICC/vEFYSfyV4qO1eS4sscV6ZI9wh
IIY3JytMcKNCuU7W2khn3MPefYUqWCGmepQA7R6EiQKBgQDXRcrsqpWSTCG+d0ga
yrf6FV2ZIAMav8kWCV0WCtMN6+teMzjpIMkCMdy1y5ySIk76iIn1EuVCd1NsSSYJ
9whJIar4p2VqZOvtI+pei9UHN3fuF72hiNbrRbqbK1EIpXyAcnOJDI4r0sf1yB07
FnCxGv+FexpcVIieKLM5UKE2TQKBgQDB7PAtJlg0riRvhxDiMNNN96iMn/K8zHtb
kW1cOcPiMUbsEL4DTPrZwXnyYjN/IoZBWnKANJb5d8jtqzsJpRiRv+zroObWGvHZ
93QlhmjKJVKqo6QMCA82sQfkSLKnV4t2PDNCWXeXOvCBGD00UXXkDgAeXz5rYD3Z
pc92vqspxwKBgQDG4ApdZCZ0FnNiI45wefFHT2+94+4aSy25dwMRNwTOKrKxvv6H
mSs6JNhy9tz5wEpYd8WDrGYyZkyikF1c/WQhM8Jgnz048m1nEjQWDnbKiPr58eLV
lbZ/elavvW/KXh/MBnAoH3pEkCD9NleS2+NWKsv/A8BRpiLkglM40v1sTQKBgQCa
bbGRctCZGvge7EMQrOsIUqkhWxo1KO0vLS8WW1XXIYCl6ms2O64jjEQtNfBmVLru
/jTiTX7QqIgUY+A+vP9Eyb7EoTxR0eE4vyr52wBFwdUD2A6CGkTrO6zaKN5EDp4b
iLMVKiPnBWOSmhfbOueTtWZ4yUXuwhRe5wDAQfQR7wKBgH8Qlb6ZCu31+pDXvood
f8VWvfm3MSiqooUORvkBgaELQGugXP8rmrn4TaoZaETsp9/Mqrh4M6F+0Y3D7JoF
nRnZsdpKgAxdKslu7ntKqTNRa8rK8lMLfbzgGOOspK84gIQgKht3nAQ0/sOayVw7
zlkiXURA4nVJBQ4Pxw/bmObi
-----END PRIVATE KEY-----
`
)

func TestBuildFullChain(t *testing.T) {
	key := decodeKey(t, []byte(serverKey))

	bootstrap := createCACertificate(t, key, "spiffe://example.org", nil)
	serverCrt := createCACertificate(t, key, "spiffe://example.org/spire/server", bootstrap)
	validCSR := makeCSR(t, key, defaultCertificateSpiffeID)
	signedCert := createClientCertificate(t, validCSR, key, serverCrt)

	privateKey := decodeKey(t, []byte(alternativeKey))
	unrelatedCA := createCACertificate(t, privateKey, "spiffe://example.org/unrelated", nil)

	testCases := map[string]struct {
		certChain     []*x509.Certificate
		trustBundle   []*x509.Certificate
		expectedErr   string
		expectedChain []*x509.Certificate
	}{
		"Empty certificate chain": {
			certChain:   []*x509.Certificate{},
			expectedErr: "empty certificate chain",
		},
		"Valid chain": {
			certChain:     []*x509.Certificate{signedCert, serverCrt},
			trustBundle:   []*x509.Certificate{bootstrap},
			expectedChain: []*x509.Certificate{signedCert, serverCrt, bootstrap},
		},
		"Fail to verify chain": {
			certChain:   []*x509.Certificate{signedCert, serverCrt},
			trustBundle: []*x509.Certificate{},
			expectedErr: "x509: certificate signed by unknown authority",
		},
		"Unrelated ca filtered": {
			certChain:     []*x509.Certificate{signedCert, serverCrt},
			trustBundle:   []*x509.Certificate{unrelatedCA, serverCrt, bootstrap},
			expectedChain: []*x509.Certificate{signedCert, serverCrt, bootstrap},
		},
	}

	for id, tc := range testCases {
		t.Run(id, func(t *testing.T) {
			chain, err := buildFullChain(tc.certChain, tc.trustBundle)
			if err != nil {
				if err.Error() != tc.expectedErr {
					t.Errorf("error (%s) does not match expected error (%s)", err.Error(), tc.expectedErr)
				}
			} else {
				if tc.expectedErr != "" {
					t.Errorf("expect error: %s but got no error", tc.expectedErr)
				} else if !reflect.DeepEqual(chain, tc.expectedChain) {
					t.Errorf("resp: got %+v, expected %v", chain, tc.expectedChain)
				}

			}

		})
	}
}

func TestNewSpireClientFail(t *testing.T) {
	key := decodeKey(t, []byte(serverKey))

	validCSR := makeCSR(t, key, defaultCertificateSpiffeID)
	bootstrap := createCACertificate(t, key, "spiffe://example.org", nil)
	tlsCert := createCACertificate(t, key, "spiffe://example.org/spire/server", bootstrap)

	s, addr := createTestServer(t, bootstrap, tlsCert, key, &mockSpireNodeServer{}, "")
	defer s.Stop()

	testCases := map[string]struct {
		bootstrapCert []byte
	}{
		"Empty bootstrap": {
			bootstrapCert: []byte(""),
		},
		"Invalid bootstrap": {
			bootstrapCert: []byte(validCSR),
		},
	}

	for id, tc := range testCases {
		t.Run(id, func(t *testing.T) {
			cli, err := NewSpireClient(addr, tc.bootstrapCert, "example.org")
			if err == nil {
				t.Error("server expected to fail")
			}
			if cli != nil {
				t.Error("no client expected when creating client fail")
			}
		})
	}
}

func TestValidateWorkloadID(t *testing.T) {
	cli := &spireClient{trustDomain: "example.org"}

	testCases := map[string]struct {
		id          string
		expectedErr string
	}{
		"Invalid scheme": {
			id:          "http://localhost/path",
			expectedErr: "invalid scheme",
		},
		"User info not allowed": {
			id:          "spiffe://user:password@example.org/path",
			expectedErr: "user info is not allowed",
		},
		"Empty trust domain": {
			id:          "spiffe:///path",
			expectedErr: "trust domain is empty",
		},
		"Port not allowed": {
			id:          "spiffe://example.org:1234/path",
			expectedErr: "port is not allowed",
		},
		"Fragment not allowed": {
			id:          "spiffe://example.org/path#fragment",
			expectedErr: "fragment is not allowed",
		},
		"Query not allowed": {
			id:          "spiffe://example.org/path?query=1",
			expectedErr: "query is not allowed",
		},
		"Unexpected trustdomain": {
			id:          "spiffe://another.org/path",
			expectedErr: "\"spiffe://another.org/path\" does not belong to trust domain \"example.org\"",
		},
		"Empty path": {
			id:          "spiffe://example.org",
			expectedErr: "path must not be empty",
		},
		"SPIRE path": {
			id:          "spiffe://example.org/spire/test",
			expectedErr: "the path cannot be part of the SPIRE reserved namespace",
		},
	}

	for id, tc := range testCases {
		t.Run(id, func(t *testing.T) {
			spiffeID, err := url.Parse(tc.id)
			if err != nil {
				t.Errorf("failed to parse id: %v", err)
				return
			}

			err = cli.validateWorkloadID(spiffeID)
			if err != nil {
				if err.Error() != tc.expectedErr {
					t.Errorf("error (%s) does not match expected error (%s)", err.Error(), tc.expectedErr)
				}
			} else {
				if tc.expectedErr != "" {
					t.Errorf("expect error: %s but got no error", tc.expectedErr)
				}
			}
		})

	}
}

func TestNewSpireClient(t *testing.T) {
	key := decodeKey(t, []byte(serverKey))

	validCSR := makeCSR(t, key, defaultCertificateSpiffeID)
	csrWithoutSAN := makeCSR(t, key, "")

	bootstrap := createCACertificate(t, key, "spiffe://example.org", nil)
	bootstrapCert := []byte(certChainPEM(bootstrap.Raw))

	bundleCert := createCACertificate(t, key, "spiffe://example.org/spire/server", bootstrap)
	successResp := createClientCertificate(t, validCSR, key, bundleCert)

	successResponse := certChainPEM(successResp.Raw)
	serverCert := certChainPEM(bundleCert.Raw)

	newKey := decodeKey(t, []byte(alternativeKey))
	invalidCA := createCACertificate(t, newKey, "spiffe://example.org/invalid", nil)

	testCases := map[string]struct {
		server         mockSpireNodeServer
		signCert       string
		expectedCert   []string
		expectedErr    string
		trustDomain    string
		tlsCertificate *x509.Certificate
	}{
		"Valid certs": {
			server:       mockSpireNodeServer{Svids: [][]byte{successResp.Raw}, CAs: [][]byte{bundleCert.Raw}},
			expectedCert: []string{successResponse, serverCert},
			signCert:     validCSR,
			expectedErr:  "",
			trustDomain:  "example.org",
		},
		"Stream fail": {
			server:         mockSpireNodeServer{Svids: [][]byte{successResp.Raw}, CAs: [][]byte{bundleCert.Raw}},
			expectedCert:   []string{successResponse, serverCert},
			signCert:       validCSR,
			expectedErr:    "failed opening stream to attestating SPIRE server",
			trustDomain:    "example.org",
			tlsCertificate: invalidCA,
		},
		"Invalid CA returned": {
			server:       mockSpireNodeServer{Svids: [][]byte{successResp.Raw}, CAs: [][]byte{invalidCA.Raw}},
			expectedCert: []string{successResponse, serverCert},
			signCert:     validCSR,
			expectedErr:  "failed to build certificate chain from SPIRE server",
			trustDomain:  "example.org",
		},
		"Svid returned with invalid key": {
			server: mockSpireNodeServer{
				Svids:        [][]byte{successResp.Raw},
				CAs:          [][]byte{bundleCert.Raw},
				Err:          nil,
				CertSpiffeID: "spiffe://example.org/anotherSvid",
			},
			signCert:    validCSR,
			expectedErr: "SPIRE response does not contains a SVID for spiffe://example.org/ns/istio-system/sa/istio-ingressgateway-service-account",
			trustDomain: "example.org",
		},
		"No bundle returned": {
			server:      mockSpireNodeServer{Svids: [][]byte{successResp.Raw}, Err: nil},
			signCert:    validCSR,
			expectedErr: "no bundle returned from SPIRE",
			trustDomain: "example.org",
		},
		"Fail decode certificate": {
			server:      mockSpireNodeServer{Svids: [][]byte{successResp.Raw}, CAs: [][]byte{bundleCert.Raw}, Err: nil},
			signCert:    "123",
			expectedErr: "could not decode csr to be signed by SPIRE",
			trustDomain: "example.org",
		},
		"Fail attestation": {
			server:      mockSpireNodeServer{Svids: [][]byte{successResp.Raw}, CAs: [][]byte{bundleCert.Raw}, Err: errors.New("something")},
			signCert:    validCSR,
			expectedErr: "attesting to SPIRE server: rpc error: code = Unknown desc = something",
			trustDomain: "example.org",
		},
		"No svid returned": {
			server:      mockSpireNodeServer{Svids: [][]byte{}, CAs: [][]byte{bundleCert.Raw}, Err: nil},
			signCert:    validCSR,
			expectedErr: "no svid returned from attestation call to SPIRE server",
			trustDomain: "example.org",
		},
		"Invalid svid": {
			server:      mockSpireNodeServer{Svids: [][]byte{[]byte("foo")}, CAs: [][]byte{bundleCert.Raw}, Err: nil},
			signCert:    validCSR,
			expectedErr: "failed to parse svid from SPIRE",
			trustDomain: "example.org",
		},
		"Invalid spiffeID in CSR": {
			server:       mockSpireNodeServer{Svids: [][]byte{successResp.Raw}, CAs: [][]byte{bundleCert.Raw}, Err: nil},
			expectedCert: []string{successResponse, string(bootstrapCert)},
			signCert:     validCSR,
			expectedErr:  "spiffe id from CSR is not valid",
			trustDomain:  "another.org",
		},
		"Certificate has not SAN": {
			server:      mockSpireNodeServer{Svids: [][]byte{successResp.Raw}, CAs: [][]byte{bundleCert.Raw}, Err: nil},
			signCert:    csrWithoutSAN,
			expectedErr: "signing certificate must contain exactly one SPIFFE ID",
			trustDomain: "example.org",
		},
	}

	for id, tc := range testCases {
		t.Run(id, func(t *testing.T) {
			tlsCertificate := tc.tlsCertificate
			if tlsCertificate == nil {
				tlsCertificate = bundleCert
			}

			// create a local grpc server
			s, addr := createTestServer(t, bootstrap, tlsCertificate, key, &tc.server, id)
			defer s.Stop()

			cli, err := NewSpireClient(addr, bootstrapCert, tc.trustDomain)
			if err != nil {
				t.Errorf("failed to create spire ca client: %v", err)
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			resp, err := cli.CSRSign(ctx, []byte(tc.signCert), fakeToken, 1)

			if err != nil {
				if err.Error() != tc.expectedErr {
					t.Errorf("error (%s) does not match expected error (%s)", err.Error(), tc.expectedErr)
				}
			} else {
				if tc.expectedErr != "" {
					t.Errorf("expect error: %s but got no error", tc.expectedErr)
				} else if !reflect.DeepEqual(resp, tc.expectedCert) {
					t.Errorf("resp: got %+v, expected %v", resp, tc.expectedCert)
				}
			}
		})
	}
}

func TestUpdateBootstrap(t *testing.T) {
	key := decodeKey(t, []byte(serverKey))

	validCSR := makeCSR(t, key, defaultCertificateSpiffeID)

	bootstrap := createCACertificate(t, key, "spiffe://example.org", nil)
	bootstrapCert := certChainPEM(bootstrap.Raw)

	validCA := createCACertificate(t, key, "spiffe://example.org/spire/server", bootstrap)

	newCA := createCACertificate(t, key, "spiffe://example.org/spire/server", bootstrap)
	secundaryCA := createCACertificate(t, key, "spiffe://example.org/secundary", bootstrap)

	successResp := createClientCertificate(t, validCSR, key, validCA)

	var expectedCA string

	bundles := []*x509.Certificate{newCA, secundaryCA}
	sortBundleCerts(bundles)

	// create expected string for latest bundle
	for _, bundle := range bundles {
		expectedCA += certChainPEM(bundle.Raw)
	}

	// create a server
	server := &mockSpireNodeServer{
		Svids: [][]byte{successResp.Raw},
		CAs:   [][]byte{newCA.Raw, secundaryCA.Raw},
	}

	s, addr := createTestServer(t, bootstrap, validCA, key, server, "")
	defer s.Stop()

	cli, err := NewSpireClient(addr, []byte(bootstrapCert), "example.org")
	if err != nil {
		t.Errorf("failed to create spire ca client: %v", err)
		return
	}

	// validate bundle is updated when client is created successfully
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	_, err = cli.CSRSign(ctx, []byte(validCSR), fakeToken, 1)
	if err != nil {
		t.Errorf("unexpected when signing CSR error: %v", err)
	}

	sc := cli.(*spireClient)
	// latest bundle must be returned in expected order
	if sc.latestBundle != expectedCA {
		t.Errorf("latest bundle was not updated")
	}

	// update server CA with a new order
	server.CAs = [][]byte{secundaryCA.Raw, newCA.Raw}
	_, err = cli.CSRSign(ctx, []byte(validCSR), fakeToken, 1)
	if err != nil {
		t.Errorf("unexpected when signing CSR error: %v", err)
	}

	// latest bundle must not be updated
	sc = cli.(*spireClient)
	if sc.latestBundle != expectedCA {
		t.Errorf("latest bundle was updated")
	}
}

// mockSpireNodeServer is a mock SPIRE server to validate Attestation
type mockSpireNodeServer struct {
	// SVIDS to be returned in attestation response
	Svids [][]byte
	// CAs bundles to be returned in attestation response
	CAs [][]byte
	// error to be returned in case it is expected
	Err error
	// spiffeID in attestation response map
	CertSpiffeID string
	// trust domain in bundles map
	TrustDomain string
}

func (s mockSpireNodeServer) FetchX509SVID(spireNode.Node_FetchX509SVIDServer) error {
	return nil
}

func (s mockSpireNodeServer) FetchJWTSVID(context.Context,
	*spireNode.FetchJWTSVIDRequest) (*spireNode.FetchJWTSVIDResponse, error) {
	return &spireNode.FetchJWTSVIDResponse{}, nil
}

func (s mockSpireNodeServer) FetchX509CASVID(context.Context,
	*spireNode.FetchX509CASVIDRequest) (*spireNode.FetchX509CASVIDResponse, error) {

	return &spireNode.FetchX509CASVIDResponse{}, nil
}

// Attest mock attestation logic to verify attestation requests
func (s mockSpireNodeServer) Attest(stream spireNode.Node_AttestServer) error {
	if s.CertSpiffeID == "" {
		s.CertSpiffeID = defaultCertificateSpiffeID
	}
	if s.TrustDomain == "" {
		s.TrustDomain = defaultTrustDomain
	}

	// return error in case it is expected
	if s.Err != nil {
		return s.Err
	}

	req, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("could not recover attest stream: %v", err)
	}

	if req.AttestationData.Type != "istio" {
		return errors.New("unexpected attestor type")
	}

	var data IstioAttestableData
	err = json.Unmarshal(req.AttestationData.Data, &data)
	if err != nil {
		return fmt.Errorf("could not unmarshal istio attested data: %v", err)
	}

	svids := make(map[string]*spireNode.X509SVID)
	for _, svid := range s.Svids {

		svids[s.CertSpiffeID] = &spireNode.X509SVID{
			CertChain: svid,
		}
	}

	bundles := make(map[string]*spireCommon.Bundle)
	if len(s.CAs) > 0 {
		var roots []*spireCommon.Certificate
		for _, ca := range s.CAs {
			roots = append(roots, &spireCommon.Certificate{DerBytes: ca})
		}

		bundles[s.TrustDomain] = &spireCommon.Bundle{
			RootCas: roots,
		}
	}

	resp := &spireNode.AttestResponse{
		SvidUpdate: &spireNode.X509SVIDUpdate{
			Svids:   svids,
			Bundles: bundles,
		},
	}

	stream.Send(resp)
	return nil
}

// createTestServer creates a server with node attestor for testing purposes
func createTestServer(t *testing.T, bootstrapCert *x509.Certificate, cert *x509.Certificate, key crypto.PrivateKey,
	server *mockSpireNodeServer, id string) (*grpc.Server, string) {
	var tlsCertificate [][]byte
	tlsCertificate = append(tlsCertificate, cert.Raw)
	certPool := x509.NewCertPool()
	certPool.AddCert(bootstrapCert)

	lis, err := net.Listen("tcp", mockServerAddress)
	if err != nil {
		t.Fatalf("Test case [%s]: failed to listen: %v", id, err)
	}

	s := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{
				{
					Certificate: tlsCertificate,
					PrivateKey:  key,
				},
			},
			ClientCAs:  certPool,
			ClientAuth: tls.VerifyClientCertIfGiven,
		})))

	spireNode.RegisterNodeServer(s, server)
	go s.Serve(lis)

	return s, lis.Addr().String()
}

// decodeKey takes PEM encoded private key bytes and returns a private key
func decodeKey(t *testing.T, key []byte) crypto.PrivateKey {
	pemBlock, _ := pem.Decode(key)
	if pemBlock == nil {
		t.Fatal("key could not be decoded")
	}

	k, err := x509.ParsePKCS8PrivateKey(pemBlock.Bytes)
	if err != nil {
		t.Fatalf("could not parse key: %v", err)
	}

	return k
}

// makeCSR creates a CSR with provided spiffeID and private key
func makeCSR(t *testing.T, privateKey interface{}, spiffeID string) string {
	var uris []*url.URL
	if spiffeID != "" {
		uriSAN, err := url.Parse(spiffeID)
		if err != nil {
			t.Fatalf("could not parse spiffeID: %v", err)
		}
		uris = append(uris, uriSAN)
	}

	template := &x509.CertificateRequest{
		Subject: pkix.Name{
			Country:      []string{"US"},
			Organization: []string{"SPIRE"},
		},
		SignatureAlgorithm: x509.SHA512WithRSA,
		URIs:               uris,
	}

	csr, err := x509.CreateCertificateRequest(rand.Reader, template, privateKey)
	if err != nil {
		t.Fatalf("could not create certificate request: %v", err)
	}

	b := &pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csr,
	}

	return string(pem.EncodeToMemory(b))
}

// createCACertificate creates a valid CA, in case another CA is provided it will sign the new CA
func createCACertificate(t *testing.T, privateKey crypto.PrivateKey, spiffeID string, ca *x509.Certificate) *x509.Certificate {
	var uris []*url.URL
	if spiffeID != "" {
		uriSAN, err := url.Parse(spiffeID)
		if err != nil {
			t.Fatalf("could not parse spiffeID: %v", err)
		}
		uris = append(uris, uriSAN)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(0),
		Subject: pkix.Name{
			Country:      []string{"US"},
			Organization: []string{"SPIRE"},
		},
		NotAfter: now.Add(10 * time.Minute),
		URIs:     uris,

		KeyUsage: x509.KeyUsageDigitalSignature |
			x509.KeyUsageCertSign |
			x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	key := privateKey.(*rsa.PrivateKey)

	if ca == nil {
		ca = template
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("could not create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("could not parse certificate: %v", err)
	}

	return cert
}

// createClientCertificate create client certificate signed by provided ca
func createClientCertificate(t *testing.T, csrDER string, privateKey crypto.PrivateKey, ca *x509.Certificate) *x509.Certificate {
	req, err := csrFromPEM([]byte(csrDER))
	if err != nil {
		t.Fatalf("could not parse CSR to PEM: %v", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(0),
		Subject: pkix.Name{
			Country:      []string{"US"},
			Organization: []string{"SPIRE"},
		},
		NotAfter: now.Add(10 * time.Minute),
		URIs:     req.URIs,
		KeyUsage: x509.KeyUsageKeyEncipherment |
			x509.KeyUsageKeyAgreement |
			x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		BasicConstraintsValid: true,
		PublicKey:             req.PublicKey,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca, req.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("could not create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("could not parse certificate: %v", err)
	}

	return cert
}
