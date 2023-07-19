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

package endpoint

import "encoding/json"

// Mirror types from grpc to avoid pulling in a bunch of deps

const FileWatcherCertProviderName = "file_watcher"

type FileWatcherCertProviderConfig struct {
	CertificateFile   string          `json:"certificate_file,omitempty"`
	PrivateKeyFile    string          `json:"private_key_file,omitempty"`
	CACertificateFile string          `json:"ca_certificate_file,omitempty"`
	RefreshDuration   json.RawMessage `json:"refresh_interval,omitempty"`
}

// FileWatcherProvider returns the FileWatcherCertProviderConfig if one exists in CertProviders
func (b *Bootstrap) FileWatcherProvider() *FileWatcherCertProviderConfig {
	if b == nil || b.CertProviders == nil {
		return nil
	}
	for _, provider := range b.CertProviders {
		if provider.PluginName == FileWatcherCertProviderName {
			return &provider.Config
		}
	}
	return nil
}

type Bootstrap struct {
	CertProviders map[string]CertificateProvider `json:"certificate_providers,omitempty"`
}

type CertificateProvider struct {
	PluginName string                        `json:"plugin_name,omitempty"`
	Config     FileWatcherCertProviderConfig `json:"config,omitempty"`
}
