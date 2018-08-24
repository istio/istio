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

package platform

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/dgrijalva/jwt-go"
	"google.golang.org/grpc"

	"istio.io/istio/security/pkg/util"
)

// K8sVaultClientImpl is the implementation of k8s vault client.
type K8sVaultClientImpl struct {
	// Root CA cert file to validate the gRPC service in CA.
	rootCertFile string
	// The private key file
	keyFile string
	// The cert chain file
	certChainFile string
	// The service account file
	serviceAccountFile string
}

// NewK8sVaultClientImpl creates a new K8sVaultClientImpl.
func NewK8sVaultClientImpl(rootCert, key, certChain, serviceAccount string) (*K8sVaultClientImpl, error) {
	if _, err := os.Stat(rootCert); err != nil {
		return nil, fmt.Errorf("failed to create vault client root cert file %v error %v", rootCert, err)
	}
	if _, err := os.Stat(key); err != nil {
		return nil, fmt.Errorf("failed to create vault client key file %v", err)
	}
	if _, err := os.Stat(certChain); err != nil {
		return nil, fmt.Errorf("failed to create vault client certChain file %v", err)
	}
	if _, err := os.Stat(serviceAccount); err != nil {
		return nil, fmt.Errorf("failed to read vault service account file %v", err)
	}
	return &K8sVaultClientImpl{rootCert, key, certChain, serviceAccount}, nil
}

// GetDialOptions returns the GRPC dial options to connect to the CA.
func (ci *K8sVaultClientImpl) GetDialOptions() ([]grpc.DialOption, error) {
	transportCreds, err := getTLSCredentials(ci.rootCertFile,
		ci.keyFile, ci.certChainFile)
	if err != nil {
		return nil, err
	}

	var options []grpc.DialOption
	options = append(options, grpc.WithTransportCredentials(transportCreds))
	return options, nil
}

// IsProperPlatform returns whether the platform is on K8s Vault.
func (ci *K8sVaultClientImpl) IsProperPlatform() bool {
	return true
}

// GetServiceIdentity gets the service account from the cert SAN field.
func (ci *K8sVaultClientImpl) GetServiceIdentity() (string, error) {
	saBytes, err := ioutil.ReadFile(ci.serviceAccountFile)
	if err != nil {
		return "", err
	}
	//Use k8s service account as service identity
	token, _, err := new(jwt.Parser).ParseUnverified(string(saBytes[:]), &util.K8sSaClaims{})
	if err != nil {
		return "", err
	}
	claims, ok := token.Claims.(*util.K8sSaClaims)
	if !ok {
		return "", fmt.Errorf("claims are not the k8s service account claims")
	}
	if err := claims.Valid(); err != nil {
		return "", err
	}
	return "spiffe://cluster.local/ns/" + claims.Namespace + "/sa/" + claims.ServiceAccountName, nil
}

// GetAgentCredential passes the service account to Citadel to authenticate
func (ci *K8sVaultClientImpl) GetAgentCredential() ([]byte, error) {
	saBytes, err := ioutil.ReadFile(ci.serviceAccountFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read the service account file: %s", ci.serviceAccountFile)
	}
	return saBytes, nil
}

// GetCredentialType returns "vault".
func (ci *K8sVaultClientImpl) GetCredentialType() string {
	return "vault"
}
