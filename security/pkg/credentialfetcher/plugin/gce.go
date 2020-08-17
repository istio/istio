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

// This is Google plugin of credentialfetcher.
package plugin

import (
	"fmt"
	"io/ioutil"

	"cloud.google.com/go/compute/metadata"

	"istio.io/istio/pkg/security"
	"istio.io/pkg/log"
)

var (
	gcecredLog = log.RegisterScope("gcecred", "GCE credential fetcher for istio agent", 0)
)

// The plugin object.
type GCEPlugin struct {
	// aud is the unique URI agreed upon by both the instance and the system verifying the instance's identity.
	// For more info: https://cloud.google.com/compute/docs/instances/verifying-instance-identity
	aud string

	// The location to save the identity token
	jwtPath string
}

// CreateGCEPlugin creates a Google credential fetcher plugin. Return the pointer to the created plugin.
func CreateGCEPlugin(audience, jwtPath string) *GCEPlugin {
	p := &GCEPlugin{
		aud:     audience,
		jwtPath: jwtPath,
	}
	return p
}

// GetPlatformCredential fetches the GCE VM identity jwt token from its metadata server,
// and write it to jwtPath. The local copy of the token in jwtPath is used by both
// Envoy STS client and istio agent to fetch certificate and access token.
// Note: this function only works in a GCE VM environment.
func (p *GCEPlugin) GetPlatformCredential() (string, error) {
	if p.jwtPath == "" {
		return "", fmt.Errorf("jwtPath is unset")
	}
	uri := fmt.Sprintf("instance/service-accounts/default/identity?audience=%s&format=full", p.aud)
	token, err := metadata.Get(uri)
	if err != nil {
		gcecredLog.Errorf("Failed to get vm identity token from metadata server: %v", err)
		return "", err
	}
	gcecredLog.Debugf("Got GCE identity token: %d", len(token))
	tokenbytes := []byte(token)
	err = ioutil.WriteFile(p.jwtPath, tokenbytes, 0640)
	if err != nil {
		gcecredLog.Errorf("Encountered error when writing vm identity token: %v", err)
		return "", err
	}
	return token, nil
}

// GetType returns credential fetcher type.
func (p *GCEPlugin) GetType() string {
	return security.GCE
}

// GetIdentityProvider returns the name of the identity provider that can authenticate the workload credential.
// GCE idenity provider is named "GoogleComputeEngine".
func (p *GCEPlugin) GetIdentityProvider() string {
	return security.GCE
}
