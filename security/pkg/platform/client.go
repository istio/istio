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

	// Temporarily disable ID token authentication on CSR API.
	// [TODO](myidpt): enable when the Citadel authz can work correctly.
	// "cloud.google.com/go/compute/metadata"
	"google.golang.org/grpc"
)

// Client is the interface for implementing the client to access platform metadata.
type Client interface {
	GetDialOptions() ([]grpc.DialOption, error)
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
func NewClient(platform, rootCertFile, keyFile, certChainFile string) (Client, error) {
	switch platform {
	case "onprem":
		return NewOnPremClientImpl(rootCertFile, keyFile, certChainFile)
	case "gcp":
		// Temporarily disable ID token authentication on CSR API.
		// [TODO](myidpt): enable when the Citadel authz can work correctly.
		return nil, fmt.Errorf("GCP credential authentication in CSR API is disabled")
	case "aws":
		// Temporarily disable ID token authentication on CSR API.
		// [TODO](myidpt): enable when the Citadel authz can work correctly.
		return nil, fmt.Errorf("AWS credential authentication in CSR API is disabled")
	case "unspecified":
		// Temporarily disable ID token authentication on CSR API.
		// [TODO](myidpt): enable when the Citadel authz can work correctly.
		// if metadata.OnGCE() {
		//   return NewGcpClientImpl(rootCertFile, caAddr), nil
		// }
		return NewOnPremClientImpl(rootCertFile, keyFile, certChainFile)
	default:
		return nil, fmt.Errorf("invalid env %s specified", platform)
	}
}
