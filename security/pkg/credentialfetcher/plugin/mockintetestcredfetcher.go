package plugin

import (
	"istio.io/istio/pkg/security"
	"istio.io/pkg/log"
)

var (
	mockInteTestPluginLog = log.RegisterScope("mockintetestplugin", "Mock Integration credential fetcher for istio agent", 0)
)

// The plugin object.
type MockInteTestPlugin struct {
	token string
}

// CreateMockPlugin creates a mock credential fetcher plugin. Return the pointer to the created plugin.
func CreateMockInteTestPlugin(token string) *MockInteTestPlugin {
	p := &MockInteTestPlugin{
		token: token,
	}
	return p
}

// GetPlatformCredential returns a constant token string.
func (p *MockInteTestPlugin) GetPlatformCredential() (string, error) {
	mockInteTestPluginLog.Debugf("mock plugin returns a constant token.")
	return p.token, nil
}

// GetType returns credential fetcher type.
func (p *MockInteTestPlugin) GetType() string {
	return security.Mock
}

// GetIdentityProvider returns the name of the identity provider that can authenticate the workload credential.
func (p *MockInteTestPlugin) GetIdentityProvider() string {
	return "fakeIDP"
}
