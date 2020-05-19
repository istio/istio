// Copyright 2020 Istio Authors
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

package caclient

import (
	"encoding/json"
	"fmt"

	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const (
	enrollPath = ""
)

var (
	caNameENV         = env.RegisterStringVar("KEYFACTOR_CA", "", "Path to keyfactor client config")
	authTokenENV      = env.RegisterStringVar("KEYFACTOR_AUTH_TOKEN", "", "Path to keyfactor client config")
	appKeyENV         = env.RegisterStringVar("KEYFACTOR_APPKEY", "", "Path to keyfactor client config")
	caTemplateENV     = env.RegisterStringVar("KEYFACTOR_CA_TEMPLATE", "Istio", "Path to keyfactor client config")
	metadataENV       = env.RegisterStringVar("KEYFACTOR_METADATA_JSON", "", "Path to keyfactor client config")
	configLog         = log.RegisterScope("keyfactorConfig", "KeyFactor CA config", 0)
	supportedMetadata = map[string]string{
		"Cluster":      "",
		"Service":      "",
		"PodName":      "",
		"PodNamespace": "",
		"TrustDomain":  "",
		"PodIP":        "",
	}
)

// KeyfactorConfig config meta for KeyfactorCA client
type KeyfactorConfig struct {
	// CaName Name of certificate authorization
	CaName string

	// Using for authentication header
	AuthToken string

	// CaTemplate Certificate Template for enroll the new one Default is Istio
	CaTemplate string

	// AppKey ApiKey from Api Setting
	AppKey string

	EnrollPath      string
	CustomMetadatas []FieldAlias
}

// FieldAlias config alias field for keyfactor client
type FieldAlias struct {
	Name  string `json:"name"`
	Alias string `json:"alias"`
}

// LoadKeyfactorConfigFromENV load and return keyfactorCA client config from env
func LoadKeyfactorConfigFromENV() (*KeyfactorConfig, error) {

	conf := &KeyfactorConfig{
		CaName:     caNameENV.Get(),
		AuthToken:  authTokenENV.Get(),
		AppKey:     appKeyENV.Get(),
		CaTemplate: caTemplateENV.Get(),
		EnrollPath: enrollPath,
	}
	metadataJSON := []byte(metadataENV.Get())
	metadatas := make([]FieldAlias, 0)

	configLog.Infof("Load metadata config for keyfactor")
	if err := json.Unmarshal(metadataJSON, &metadatas); err != nil {
		configLog.Errorf("Cannot parse data from KEYFACTOR_METADATA_JSON (.keyfactor.metadata)")
		return nil, fmt.Errorf("Cannot parse data from KEYFACTOR_METADATA_JSON (.keyfactor.metadata): %v", err)
	}
	conf.CustomMetadatas = metadatas
	configLog.Infof("Validate Keyfactor config\n%v", conf)
	if err := conf.Validate(); err != nil {
		return nil, err
	}
	return conf, nil
}

// Validate make sure configuration is valid
func (kc *KeyfactorConfig) Validate() error {

	if kc.CaName == "" {
		return fmt.Errorf("Missing caName")
	}

	if kc.AuthToken == "" {
		return fmt.Errorf("Missing authToken")
	}

	if kc.AppKey == "" {
		return fmt.Errorf("Missing appKey")
	}

	if kc.CaTemplate == "" {
		return fmt.Errorf("Missing caTemplate")
	}

	if kc.EnrollPath == "" {
		return fmt.Errorf("Missing enrollPath")
	}

	for _, value := range kc.CustomMetadatas {
		if _, ok := supportedMetadata[value.Name]; !ok {
			return fmt.Errorf("Do not support Metadata field name: %v", value.Name)
		}
	}
	return nil
}
