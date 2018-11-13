//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package testdata

import (
	"fmt"
	"path"
	"strings"
)

// Load loads the test data set.
func Load() ([]TestInfo, error) {
	var result []TestInfo
	for _, asset := range AssetNames() {
		ext := path.Ext(asset)
		// Use the input file to drive the generation of TestInfo resources.
		if ext != ".yaml" {
			continue
		}

		baseFileName := asset[:len(asset)-len(".yaml")]

		// Skip if the asset is a meshconfig file. We will pick it up when processing the input file.
		if strings.HasSuffix(baseFileName, "_meshconfig") {
			continue
		}

		meshConfigFile := fmt.Sprintf("%s_meshconfig.yaml", baseFileName)
		expectedFile := fmt.Sprintf("%s_expected.json", baseFileName)

		// The expected file is required
		if _, err := AssetInfo(expectedFile); err != nil {
			return nil, err
		}

		// Mesh Config file is optional
		if _, err := AssetInfo(meshConfigFile); err != nil {
			meshConfigFile = ""
		}

		r := TestInfo{
			inputFile:      asset,
			expectedFile:   expectedFile,
			meshConfigFile: meshConfigFile,
			Skipped:        false,
		}
		result = append(result, r)
	}

	return result, nil
}
