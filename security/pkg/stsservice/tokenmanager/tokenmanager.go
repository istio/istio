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

package tokenmanager

import (
	"errors"
	"fmt"

	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/stsservice"
	"istio.io/istio/security/pkg/stsservice/tokenmanager/google"
)

const (
	// GoogleTokenExchange is the name of the google token exchange service.
	GoogleTokenExchange = "GoogleTokenExchange"
)

// Plugin provides common interfaces for specific token exchange services.
type Plugin interface {
	ExchangeToken(parameters stsservice.StsRequestParameters) ([]byte, error)
	DumpPluginStatus() ([]byte, error)
}

type TokenManager struct {
	plugin Plugin
}

type Config struct {
	CredFetcher security.CredFetcher
	TrustDomain string
}

// GCPProjectInfo stores GCP project information, including project number,
// project ID, cluster location, cluster name
type GCPProjectInfo struct {
	Number          string
	id              string
	cluster         string
	clusterLocation string
}

func getGCPProjectInfo() GCPProjectInfo {
	info := GCPProjectInfo{}
	if platform.IsGCP() {
		md := platform.NewGCP().Metadata()
		if projectNum, found := md[platform.GCPProjectNumber]; found {
			info.Number = projectNum
		}
		if projectID, found := md[platform.GCPProject]; found {
			info.id = projectID
		}
		if clusterName, found := md[platform.GCPCluster]; found {
			info.cluster = clusterName
		}
		if clusterLocation, found := md[platform.GCPLocation]; found {
			info.clusterLocation = clusterLocation
		}
	}
	return info
}

// CreateTokenManager creates a token manager with specified type and returns
// that token manager
func CreateTokenManager(tokenManagerType string, config Config) stsservice.TokenManager {
	tm := &TokenManager{
		plugin: nil,
	}
	switch tokenManagerType {
	case GoogleTokenExchange:
		if projectInfo := getGCPProjectInfo(); len(projectInfo.Number) > 0 {
			gkeClusterURL := fmt.Sprintf("https://container.googleapis.com/v1/projects/%s/locations/%s/clusters/%s",
				projectInfo.id, projectInfo.clusterLocation, projectInfo.cluster)
			if p, err := google.CreateTokenManagerPlugin(config.CredFetcher, config.TrustDomain, projectInfo.Number, gkeClusterURL, true); err == nil {
				tm.plugin = p
			}
		}
	}
	return tm
}

func (tm *TokenManager) GenerateToken(parameters stsservice.StsRequestParameters) ([]byte, error) {
	if tm.plugin != nil {
		return tm.plugin.ExchangeToken(parameters)
	}
	return nil, errors.New("no plugin is found")
}

func (tm *TokenManager) DumpTokenStatus() ([]byte, error) {
	if tm.plugin != nil {
		return tm.plugin.DumpPluginStatus()
	}
	return nil, errors.New("no plugin is found")
}

// SetPlugin sets token exchange plugin for testing purposes only.
func (tm *TokenManager) SetPlugin(p Plugin) {
	tm.plugin = p
}
