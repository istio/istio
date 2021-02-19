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
	"sync"
	"time"

	"cloud.google.com/go/compute/metadata"

	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/util"
	"istio.io/pkg/log"
)

var gcecredLog = log.RegisterScope("gcecred", "GCE credential fetcher for istio agent", 0)

// Token refresh frequency is default to 5 minutes.
var rotationInterval = 5 * time.Minute

// GCE VM credential needs refresh if remaining life time is below 25 minutes.
var gracePeriod = 25 * time.Minute

// rotateToken determines whether to start periodic token rotation job.
// This is enabled by default.
var rotateToken = true

// SetTokenRotation enable/disable periodic token rotation job.
// This is only for testing purpose, not thread safe.
func SetTokenRotation(enable bool) {
	rotateToken = enable
}

// The plugin object.
type GCEPlugin struct {
	// aud is the unique URI agreed upon by both the instance and the system verifying the instance's identity.
	// For more info: https://cloud.google.com/compute/docs/instances/verifying-instance-identity
	aud string

	// The location to save the identity token
	jwtPath string

	// identity provider
	identityProvider string

	// token refresh
	rotationTicker *time.Ticker
	closing        chan bool
	tokenCache     string
	// mutex lock is required to avoid race condition when updating token file and token cache.
	tokenMutex sync.RWMutex
}

// CreateGCEPlugin creates a Google credential fetcher plugin. Return the pointer to the created plugin.
func CreateGCEPlugin(audience, jwtPath, identityProvider string) *GCEPlugin {
	p := &GCEPlugin{
		aud:              audience,
		jwtPath:          jwtPath,
		identityProvider: identityProvider,
		closing:          make(chan bool),
	}
	if rotateToken {
		go p.startTokenRotationJob()
	}
	return p
}

func (p *GCEPlugin) Stop() {
	close(p.closing)
}

func (p *GCEPlugin) startTokenRotationJob() {
	// Wake up once in a while and refresh GCE VM credential.
	p.rotationTicker = time.NewTicker(rotationInterval)
	for {
		select {
		case <-p.rotationTicker.C:
			p.rotate()
		case <-p.closing:
			if p.rotationTicker != nil {
				p.rotationTicker.Stop()
			}
			return
		}
	}
}

func (p *GCEPlugin) rotate() {
	if p.shouldRotate(time.Now()) {
		if _, err := p.GetPlatformCredential(); err != nil {
			gcecredLog.Errorf("credential refresh failed: %+v", err)
		}
	}
}

func (p *GCEPlugin) shouldRotate(now time.Time) bool {
	p.tokenMutex.RLock()
	defer p.tokenMutex.RUnlock()

	if p.tokenCache == "" {
		return true
	}
	exp, err := util.GetExp(p.tokenCache)
	// When fails to get expiration time from token, always refresh the token.
	if err != nil || exp.IsZero() {
		return true
	}
	rotate := now.After(exp.Add(-gracePeriod))
	gcecredLog.Debugf("credential expiration: %s, grace period: %s, should rotate: %t",
		exp.String(), gracePeriod.String(), rotate)
	return rotate
}

// GetPlatformCredential fetches the GCE VM identity jwt token from its metadata server,
// and write it to jwtPath. The local copy of the token in jwtPath is used by both
// Envoy STS client and istio agent to fetch certificate and access token.
// Note: this function only works in a GCE VM environment.
func (p *GCEPlugin) GetPlatformCredential() (string, error) {
	p.tokenMutex.Lock()
	defer p.tokenMutex.Unlock()

	if p.jwtPath == "" {
		return "", fmt.Errorf("jwtPath is unset")
	}
	uri := fmt.Sprintf("instance/service-accounts/default/identity?audience=%s&format=full", p.aud)
	token, err := metadata.Get(uri)
	if err != nil {
		gcecredLog.Errorf("Failed to get vm identity token from metadata server: %v", err)
		return "", err
	}
	// Update token cache.
	p.tokenCache = token
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
	return p.identityProvider
}
