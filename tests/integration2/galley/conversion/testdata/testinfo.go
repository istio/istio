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
	"encoding/json"
	"fmt"
	"strings"
)

// TestInfo about a particular test.
type TestInfo struct {
	inputFile      string
	expectedFile   string
	meshConfigFile string
	Skipped        bool
}

// TestName that is generated for this test.
func (t TestInfo) TestName() string {
	name := t.inputFile[:len(t.inputFile)-len(".yaml")]
	name = name[strings.Index(name, "/")+1:]
	name = strings.Replace(name, "/", "_", -1)

	return name
}

// LoadInputFile returns the input yaml file for this test.
func (t TestInfo) LoadInputFile() ([]byte, error) {
	return Asset(t.inputFile)
}

// LoadExpectedFile returns the expected file for this test.
func (t TestInfo) LoadExpectedFile() ([]byte, error) {
	return Asset(t.expectedFile)
}

// LoadExpectedResources loads and parses the expected resources from the expected file.
func (t TestInfo) LoadExpectedResources() (map[string][]map[string]interface{}, error) {
	b, err := t.LoadExpectedFile()
	if err != nil {
		return nil, err
	}

	expectedRecord := make(map[string]interface{})
	if err := json.Unmarshal(b, &expectedRecord); err != nil {
		return nil, fmt.Errorf("error parsing expected JSON: %v", err)
	}

	result := make(map[string][]map[string]interface{})
	for url, item := range expectedRecord {
		arr, ok := item.([]interface{})
		if !ok {
			return nil, fmt.Errorf("expected JSON is not in the form of record of arrays")
		}

		var resultArray []map[string]interface{}
		for _, entry := range arr {
			m, ok := entry.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("expected JSON is not in the form of record of arrays of records")
			}
			resultArray = append(resultArray, m)
		}

		result[url] = resultArray
	}

	return result, nil
}

// HasMeshConfigFile returns true if there is a mesh config file for this test.
func (t TestInfo) HasMeshConfigFile() bool {
	return t.meshConfigFile != ""
}

// LoadMeshConfigFile returns the meshconfigfile for this test.
func (t TestInfo) LoadMeshConfigFile() ([]byte, error) {
	return Asset(t.meshConfigFile)
}