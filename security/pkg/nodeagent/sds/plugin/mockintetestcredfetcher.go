package plugin

import (
	"istio.io/istio/pkg/security"
	"istio.io/pkg/log"
)

const (
	FirstPartyJwt = "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9." +
			"eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9u" +
			"YW1lc3BhY2UiOiJmb28iLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlY3JldC5uYW1lIjoiaHR0cGJp" +
			"bi10b2tlbi14cWRncCIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50Lm5hbWUi" +
			"OiJodHRwYmluIiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZXJ2aWNlLWFjY291bnQudWlkIjoiOTlm" +
			"NjVmNTAtNzYwYS0xMWVhLTg5ZTctNDIwMTBhODAwMWMxIiwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50OmZv" +
			"bzpodHRwYmluIn0.4kIl9TRjXEw6DfhtR-LdpxsAlYjJgC6Ly1DY_rqYY4h0haxcXB3kYZ3b2He-3fqOBryz524W" +
			"KkZscZgvs5L-sApmvlqdUG61TMAl7josB0x4IMHm1NS995LNEaXiI4driffwfopvqc_z3lVKfbF9j-mBgnCepxz3" +
			"UyWo5irFa3qcwbOUB9kuuUNGBdtbFBN5yIYLpfa9E-MtTX_zJ9fQ9j2pi8Z4ljii0tEmPmRxokHkmG_xNJjUkxKU" +
			"WZf4bLDdCEjVFyshNae-FdxiUVyeyYorTYzwZZYQch9MJeedg4keKKUOvCCJUlKixd2qAe-H7r15RPmo4AU5O5YL" +
			"65xiNg"
)
var (
	mockInteTestPluginLog = log.RegisterScope("mockintetestplugin", "Mock Integration credential fetcher for istio agent", 0)
)


// The plugin object.
type MockInteTestPlugin struct {
}

// CreateMockPlugin creates a mock credential fetcher plugin. Return the pointer to the created plugin.
func CreateMockInteTestPlugin() *MockInteTestPlugin {
	p := &MockInteTestPlugin{}
	return p
}

// GetPlatformCredential returns a constant token string.
func (p *MockInteTestPlugin) GetPlatformCredential() (string, error) {
	mockInteTestPluginLog.Debugf("mock plugin returns a constant token.")
	return FirstPartyJwt, nil
}

// GetType returns credential fetcher type.
func (p *MockInteTestPlugin) GetType() string {
	return security.Mock
}

// GetIdentityProvider returns the name of the identity provider that can authenticate the workload credential.
func (p *MockInteTestPlugin) GetIdentityProvider() string {
	return "fakeIDP"
}

