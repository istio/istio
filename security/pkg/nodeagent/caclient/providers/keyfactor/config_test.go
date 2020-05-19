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
	"fmt"
	"os"
	"testing"
)

func TestKeyfactorConfigFromFile(t *testing.T) {

	testCases := map[string]struct {
		configName  string
		expectedErr string
	}{
		"config.yaml file missing": {
			configName:  "./testdata/nothing.yaml",
			expectedErr: fmt.Sprintf("Missing keyfactor config at: %v", "testdata/nothing.yaml"),
		},
		"Invalid metadata field name": {
			configName:  "./testdata/config_invalid_metadata.yaml",
			expectedErr: fmt.Sprintf("Do not support Metadata field name: %v", "ServiceINVALID"),
		},
		"Should show error: Missing appkey": {
			configName:  "./testdata/config_missing_appkey.yaml",
			expectedErr: fmt.Sprintf("Missing client.appKey in: %v", "testdata/config_missing_appkey.yaml"),
		},
		"Should show error: Missing authToken": {
			configName:  "./testdata/config_missing_authtoken.yaml",
			expectedErr: fmt.Sprintf("Missing client.authToken in: %v", "testdata/config_missing_authtoken.yaml"),
		},
		"Should show error: Missing caName": {
			configName:  "./testdata/config_missing_caname.yaml",
			expectedErr: fmt.Sprintf("Missing client.caName in: %v", "testdata/config_missing_caname.yaml"),
		},
		"Should show error: Missing enrollPath": {
			configName:  "./testdata/config_missing_enrollpath.yaml",
			expectedErr: fmt.Sprintf("Missing client.enrollPath in: %v", "testdata/config_missing_enrollpath.yaml"),
		},
		"Should show error: Missing caTemplate": {
			configName:  "./testdata/config_missing_catemplate.yaml",
			expectedErr: fmt.Sprintf("Missing client.caTemplate in: %v", "testdata/config_missing_catemplate.yaml"),
		},
		"Invalid structure of yaml": {
			configName:  "./testdata/invalid_structure.yaml",
			expectedErr: fmt.Sprintf("Missing client.caName in: %v", "testdata/invalid_structure.yaml"),
		},
	}

	for testID, testcase := range testCases {
		t.Run(testID, func(tsub *testing.T) {

			os.Setenv("KEYFACTOR_CONFIG_PATH", ".")
			defer os.Unsetenv("KEYFACTOR_CONFIG_PATH")

			os.Setenv("KEYFACTOR_CONFIG_NAME", testcase.configName)
			defer os.Unsetenv("KEYFACTOR_CONFIG_NAME")

			_, err := LoadKeyfactorConfigFromENV()

			if err != nil && err.Error() != testcase.expectedErr {
				tsub.Errorf("Failed testcase: %s - Expect (%v), but got (%v)", testID, testcase.expectedErr, err)
			}
		})
	}
}
