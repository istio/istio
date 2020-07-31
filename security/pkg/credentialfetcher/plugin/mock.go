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
)

// The plugin object.
type MockPlugin struct {
	// Log scope
	credlog *log.Scope
}

// CreateMockPlugin creates a mock credential fetcher plugin. Return the pointer to the created plugin.
func CreateMockPlugin(scope *log.Scope) *MockPlugin {
	p := &MockPlugin{
		credlog: scope,
	}
	return p
}

// GetPlatformCredential returns a constant token string.
func (p *MockPlugin) GetPlatformCredential() (string, error) {
	p.credlog.Debugf("mock plugin returns a constant token.")
	return "test_token", nil
}
