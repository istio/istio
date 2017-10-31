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

package mock

import (
	"fmt"

	"google.golang.org/grpc"
	"istio.io/istio/auth/pkg/platform"
)

// FakeClient is mocked platform metadata client.
type FakeClient struct {
	DialOption     []grpc.DialOption
	DialOptionErr  string
	Identity       string
	IdentityErr    string
	ProperPlatform bool
}

// GetDialOptions returns the DialOption field.
func (f FakeClient) GetDialOptions(*platform.ClientConfig) ([]grpc.DialOption, error) {
	if len(f.DialOptionErr) > 0 {
		return nil, fmt.Errorf(f.DialOptionErr)
	}

	return f.DialOption, nil
}

// GetServiceIdentity returns IdentityErr, or the Identity if IdentityErr is nil.
func (f FakeClient) GetServiceIdentity() (string, error) {
	if len(f.IdentityErr) > 0 {
		return "", fmt.Errorf(f.IdentityErr)
	}

	return f.Identity, nil
}

// IsProperPlatform returns ProperPlatform.
func (f FakeClient) IsProperPlatform() bool {
	return f.ProperPlatform
}

// GetAgentCredential returns empty credential.
func (f FakeClient) GetAgentCredential() ([]byte, error) {
	return []byte{}, nil
}

// GetCredentialType returns "fake".
func (f FakeClient) GetCredentialType() string {
	return "fake"
}
