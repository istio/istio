// Copyright 2017 Istio Authors
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
	"fmt"
	"io/ioutil"

	"golang.org/x/net/context"

	"istio.io/istio/security/pkg/util"
	pb "istio.io/istio/security/proto"
)

// Config for Vault prototyping purpose
const (
	//signCsrPath        = "istio_ca/sign-verbatim"
	signCsrPath        = "istio_ca/sign/istio-pki-role"
	signCsrRole        = "istio-cert"
	serviceAccountPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	caCertPath         = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	k8sAPIServerURL    = "https://kubernetes.default.svc/apis/authentication.k8s.io/v1/tokenreviews"
	vaultAuthloginPath = "auth/kubernetes/login"
)

// VaultCa implements IstioCAServiceServer and provides the service on the
// specified port.
type VaultCa struct {
	// Server
	// Vault server IP address
	vaultIP string
	// Vault server port
	vaultPort int
	// Path to the SA reviewer's token
	reviewerTokenPath string
}

// HandleCSR handles an incoming certificate signing request (CSR). It does
// proper validation (e.g. authentication) and upon validated, signs the CSR
// and returns the resulting certificate. If not approved, reason for refusal
// to sign is returned as part of the response object.
func (s *VaultCa) HandleCSR(ctx context.Context, request *pb.CsrRequest) (*pb.CsrResponse, error) {
	reviewerToken, err := ioutil.ReadFile(s.reviewerTokenPath)
	if err != nil {
		panic(fmt.Sprintf("Failed to read the reviewer's token: %v", err))
	}
	// Read the CA certificate of the k8s API server
	k8sCaCert, err := ioutil.ReadFile(caCertPath)
	if err != nil {
		panic(fmt.Sprintf("Failed to read the CA certificate of k8s API server: %v", err))
	}
	sa, err := ioutil.ReadFile(serviceAccountPath)
	if err != nil {
		panic(fmt.Sprintf("Failed to read Istio CA service account: %v", err))
	}

	if request.CredentialType != "vault" {
		return nil, fmt.Errorf("invalid credential type: %v", request.CredentialType)
	}
	err = util.ValidateCsrRequest(k8sAPIServerURL, k8sCaCert, string(reviewerToken[:]), string(request.CsrPem[:]),
		request.NodeAgentCredential)
	if err != nil {
		return nil, fmt.Errorf("failed to validate CSR: %v", err)
	}

	vaultAddr := fmt.Sprintf("http://%s:%d", s.vaultIP, s.vaultPort)
	client, err := util.CreateVaultClient(vaultAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to create a Vault connection: %v", err)
	}
	token, err := util.LoginVaultK8sAuthMethod(client, vaultAuthloginPath, signCsrRole, string(sa[:]))
	if err != nil {
		return nil, fmt.Errorf("failed to login Vault: %v", err)
	}
	client.SetToken(token)
	cert, certChain, err := util.SignCsrByVault(client, signCsrPath, request.CsrPem[:])
	if err != nil {
		return nil, fmt.Errorf("failed to sign CSR: %v", err)
	}

	response := &pb.CsrResponse{
		IsApproved: true,
		SignedCert: cert,
		CertChain:  certChain,
	}
	return response, nil
}

// NewVaultCa creates a new instance of `IstioCAServiceServer`.
func NewVaultCa(vaultIP string, vaultPort int, reviewerTokenPath string) *VaultCa {
	return &VaultCa{
		vaultIP:           vaultIP,
		vaultPort:         vaultPort,
		reviewerTokenPath: reviewerTokenPath,
	}
}
