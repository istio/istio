// Copyright Istio Authors
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

// Test only: this is the mock plugin of credentialfetcher.
package plugin

import (
	"istio.io/pkg/log"

	"istio.io/istio/pkg/security"
)

var (
	mockcredLog = log.RegisterScope("mockcred", "Mock credential fetcher for istio agent", 0)
)

// The plugin object.
type MockPlugin struct {
}

// CreateMockPlugin creates a mock credential fetcher plugin. Return the pointer to the created plugin.
func CreateMockPlugin() *MockPlugin {
	p := &MockPlugin{}
	return p
}

// GetPlatformCredential returns a constant token string.
func (p *MockPlugin) GetPlatformCredential() (string, error) {
	mockcredLog.Debugf("mock plugin returns a constant token.")
	return "test_token", nil
}

// GetType returns credential fetcher type.
func (p *MockPlugin) GetType() string {
	return security.Mock
}

// GetIdentityProvider returns the name of the identity provider that can authenticate the workload credential.
func (p *MockPlugin) GetIdentityProvider() string {
	return "fakeIDP"
}
