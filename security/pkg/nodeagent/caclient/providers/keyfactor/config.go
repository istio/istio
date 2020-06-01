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
	"io/ioutil"

	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const (
	enrollPath = "KeyfactorAPI/Enrollment/CSR"
)

var (
	metadataENV       = env.RegisterStringVar("KEYFACTOR_METADATA_JSON", "", "Metadata configuration as JSON")
	configPathENV     = env.RegisterStringVar("KEYFACTOR_CONFIG_PATH", "/etc/keyfactor/config.json", "Path to keyfactor client config")
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
	CaName string `json:"caName"`

	// Using for authentication header
	AuthToken string `json:"authToken"`

	// CaTemplate Certificate Template for enroll the new one Default is Istio
	CaTemplate string `json:"caTemplate"`

	// AppKey ApiKey from Api Setting
	AppKey string `json:"appKey"`

	// EnrollPath api path to Enroll CSR Request
	EnrollPath string

	// CustomMetadatas configure enable of name of metadata fields
	CustomMetadatas []FieldAlias
}

// FieldAlias config alias field for keyfactor client
type FieldAlias struct {
	Name  string `json:"name"`
	Alias string `json:"alias"`
}

// LoadKeyfactorConfigFile load and return keyfactorCA client config from env
func LoadKeyfactorConfigFile() (*KeyfactorConfig, error) {
	configFilePath := configPathENV.Get()

	bconfig, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("unable to read keyfactor config file (%s): %v. <missing or empty secret>", configFilePath, err)
	}

	conf := &KeyfactorConfig{
		EnrollPath: enrollPath,
	}

	if err := json.Unmarshal(bconfig, &conf); err != nil {
		configLog.Errorf("cannot parse keyfactor config file (%s): %v", configFilePath, err)
		return nil, fmt.Errorf("cannot parse keyfactor config file (%s): %v", configFilePath, err)
	}

	metadatas := make([]FieldAlias, 0)
	metadataJSONfromENV := []byte(metadataENV.Get())
	if err := json.Unmarshal(metadataJSONfromENV, &metadatas); err != nil {
		configLog.Errorf("cannot parse metadata configuration (%s): %v", metadatas, err)
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
		return fmt.Errorf("missing caName (KEYFATOR_CA) in ENV")
	}

	if kc.AuthToken == "" {
		return fmt.Errorf("missing authToken (KEYFATOR_AUTH_TOKEN) in ENV")
	}

	if kc.AppKey == "" {
		return fmt.Errorf("missing appKey (KEYFATOR_APPKEY) in ENV")
	}

	if kc.CaTemplate == "" {
		return fmt.Errorf("missing caTemplate (KEYFATOR_CA_TEMPLATE) in ENV")
	}

	configLog.Infof("Validating custom Metadata")

	for _, value := range kc.CustomMetadatas {
		configLog.Infof("Validating fieldName: %v", value.Name)
		if _, found := supportedMetadata[value.Name]; !found {
			configLog.Errorf("do not support Metadata field name: %v", value.Name)
			return fmt.Errorf("do not support Metadata field name: %v", value.Name)
		}

		if value.Alias == "" {
			return fmt.Errorf("invalid alias name for Metadata: %v", value.Name)
		}
	}
	return nil
}
