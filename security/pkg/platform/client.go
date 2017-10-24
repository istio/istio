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

package platform

import (
	"fmt"

	"google.golang.org/grpc"
)

// ClientConfig consists of the platform client configuration.
type ClientConfig struct {
	// Root CA cert file to validate the gRPC service in CA.
	RootCACertFile string
	// The private key file
	KeyFile string
	// The cert chain file
	CertChainFile string
}

// Client is the interface for implementing the client to access platform metadata.
type Client interface {
	GetDialOptions(*ClientConfig) ([]grpc.DialOption, error)
	// Whether the node agent is running on the right platform, e.g., if gcpPlatformImpl should only
	// run on GCE.
	IsProperPlatform() bool
	// Get the service identity.
	GetServiceIdentity() (string, error)
	// Get node agent credential
	GetAgentCredential() ([]byte, error)
	// Get type of the credential
	GetCredentialType() string
}

// NewClient is the function to create implementations of the platform metadata client.
func NewClient(platform string, config ClientConfig, caAddr string) (Client, error) {
	switch platform {
	case "onprem":
		return NewOnPremClientImpl(config.CertChainFile), nil
	case "gcp":
		return NewGcpClientImpl(caAddr), nil
	case "aws":
		return NewAwsClientImpl(), nil
	default:
		return nil, fmt.Errorf("Invalid env %s specified", platform)
	}
}
