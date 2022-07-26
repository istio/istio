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

package plugin

import (
	"os"
	"strings"

	"istio.io/istio/pkg/security"
	"istio.io/pkg/log"
)

type KubernetesTokenPlugin struct {
	path string
}

var _ security.CredFetcher = &KubernetesTokenPlugin{}

func CreateTokenPlugin(path string) *KubernetesTokenPlugin {
	return &KubernetesTokenPlugin{
		path: path,
	}
}

func (t KubernetesTokenPlugin) GetPlatformCredential() (string, error) {
	if t.path == "" {
		return "", nil
	}
	tok, err := os.ReadFile(t.path)
	if err != nil {
		log.Warnf("failed to fetch token from file: %v", err)
		return "", nil
	}
	return strings.TrimSpace(string(tok)), nil
}

func (t KubernetesTokenPlugin) GetIdentityProvider() string {
	return ""
}

func (t KubernetesTokenPlugin) Stop() {
}
