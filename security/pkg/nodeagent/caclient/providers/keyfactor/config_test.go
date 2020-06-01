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
	"os"
	"testing"
)

func TestKeyfactorConfigFromFile(t *testing.T) {

	testCases := map[string]struct {
		configPath      string
		customMetadatas map[string]string
		expectedErr     string
	}{
		"Missing caName in ENV": {
			configPath:      "./testdata/missing_caName.json",
			customMetadatas: map[string]string{"Cluster": "Fake_Alias_Cluster"},
			expectedErr:     "missing caName (KEYFATOR_CA) in ENV",
		},
		"Missing appKey in ENV": {
			configPath:      "./testdata/missing_app_key.json",
			customMetadatas: map[string]string{"Cluster": "Fake_Alias_Cluster"},
			expectedErr:     "missing appKey (KEYFATOR_APPKEY) in ENV",
		},
		"Missing authToken in ENV": {
			configPath:      "./testdata/missing_auth_token.json",
			customMetadatas: map[string]string{"Cluster": "Fake_Alias_Cluster"},
			expectedErr:     "missing authToken (KEYFATOR_AUTH_TOKEN) in ENV",
		},
		"Missing caTemplate in ENV": {
			configPath:      "./testdata/missing_caTemplate.json",
			customMetadatas: map[string]string{"Cluster": "Fake_Alias_Cluster"},
			expectedErr:     "missing caTemplate (KEYFATOR_CA_TEMPLATE) in ENV",
		},
		"Do not supported new metadata field": {
			configPath:      "./testdata/valid.json",
			customMetadatas: map[string]string{"Cluster": "Fake_Alias_Cluster"},
			expectedErr:     "do not support Metadata field name: ClusterInvalid",
		},
		"Empty metadata configuration": {
			configPath:      "./testdata/valid.json",
			customMetadatas: nil,
			expectedErr:     "",
		},
		"Invalid json config file": {
			configPath:      "./testdata/invalid.json",
			customMetadatas: nil,
			expectedErr: "cannot parse keyfactor config file (./testdata/invalid.json): " +
				"invalid character 'i' looking for beginning of value",
		},
		"Missing json config file": {
			configPath:      "./testdata/nowhere.json",
			customMetadatas: nil,
			expectedErr: "unable to read keyfactor config file (./testdata/nowhere.json):" +
				" open ./testdata/nowhere.json: no such file or directory. <missing or empty secret>",
		},
		"Valid configuration json file": {
			configPath:      "./testdata/valid.json",
			customMetadatas: map[string]string{"Cluster": "Fake_Alias_Cluster"},
			expectedErr:     "",
		},
	}

	for testID, tc := range testCases {
		t.Run(testID, func(tsub *testing.T) {
			os.Setenv("KEYFACTOR_CONFIG_PATH", tc.configPath)

			metadataJSON, _ := json.Marshal(tc.customMetadatas)
			os.Setenv("KEYFACTOR_METADATA_JSON", string(metadataJSON))

			defer func() {
				os.Unsetenv("KEYFACTOR_CONFIG_PATH")
				os.Unsetenv("KEYFACTOR_METADATA_JSON")
			}()

			_, err := LoadKeyfactorConfigFile()

			if err != nil && err.Error() != tc.expectedErr {
				tsub.Errorf("Failed testcase: %s - Expect (%v), but got (%v)", testID, tc.expectedErr, err)
			}
		})
	}
}
