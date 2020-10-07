package plugin

import (
	"istio.io/istio/pkg/security"
	"istio.io/pkg/log"

)

var (
	mockInteTestPluginLog = log.RegisterScope("mockintetestplugin", "Mock Integration credential fetcher for istio agent", 0)

	// thirdPartyJwt is generated in a testing K8s cluster, using the "istio-token" projected volume.
	// Token is issued at 2020-04-04 22:13:54 Pacific Daylight time.
	// Expiration time is 2020-04-05 10:13:54 Pacific Daylight time.
	thirdPartyJwt = "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9." +
			"eyJhdWQiOlsieW9uZ2dhbmdsLWlzdGlvLTQuc3ZjLmlkLmdvb2ciXSwiZXhwIjoxNTg2MTA2ODM0LCJpYXQiOjE1" +
			"ODYwNjM2MzQsImlzcyI6Imh0dHBzOi8vY29udGFpbmVyLmdvb2dsZWFwaXMuY29tL3YxL3Byb2plY3RzL3lvbmdn" +
			"YW5nbC1pc3Rpby00L2xvY2F0aW9ucy91cy1jZW50cmFsMS1hL2NsdXN0ZXJzL2NsdXN0ZXItMyIsImt1YmVybmV0" +
			"ZXMuaW8iOnsibmFtZXNwYWNlIjoiZm9vIiwicG9kIjp7Im5hbWUiOiJodHRwYmluLTY0Nzc2YmY3OGQtanFsNWIi" +
			"LCJ1aWQiOiI5YWQ3NTcxYi03NjBhLTExZWEtODllNy00MjAxMGE4MDAxYzEifSwic2VydmljZWFjY291bnQiOnsi" +
			"bmFtZSI6Imh0dHBiaW4iLCJ1aWQiOiI5OWY2NWY1MC03NjBhLTExZWEtODllNy00MjAxMGE4MDAxYzEifX0sIm5i" +
			"ZiI6MTU4NjA2MzYzNCwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50OmZvbzpodHRwYmluIn0.XWSCdarBR0cx" +
			"MlVV5X9pvgI9m0lyO-17B45aBKJBIQjvjluKXqxnCuIeD3X2ItLkCUzmKUa3ftTjUUov1MJ89MdBngNfUP7IfwnD" +
			"2dBl7Jtju0-Ks7aTFOkgtoMYqNnQ1VSDTAOfNpdZVUnsR_oY8obXSQR_H4uMcaNOGED2RX5HLBWFlvymtn4JXuyg" +
			"_rpOrJ8dv-snrmO3LT9y-zaUnZqSceDC8skzStrJIRvsRkO8GEcoQd5VwDn-UVgOcqWb-S-vgSjdtwBsnGPXsh_I" +
			"NZCq3ftr0Qu8-IxsIjpMjhLmAGTH1bR324aqLTYhAXp6fk06Pe3T9stCY5acSeadKA"

	// firstPartyJwt is generated in a testing K8s cluster. It is the default service account JWT.
	// No expiration time.
	firstPartyJwt = "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9." +
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
	return firstPartyJwt, nil
}

// GetType returns credential fetcher type.
func (p *MockInteTestPlugin) GetType() string {
	return security.Mock
}

// GetIdentityProvider returns the name of the identity provider that can authenticate the workload credential.
func (p *MockInteTestPlugin) GetIdentityProvider() string {
	return "fakeIDP"
}

