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
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
)

// TestInfo about a particular test.
type TestInfo struct {
	testName string
	files    []*FileSet
	Skipped  bool
}

// FileSet is the set of files to apply at each stage of running a test.
type FileSet struct {
	inputFile      string
	expectedFile   string
	meshConfigFile string
}

// TestName that is generated for this test.
func (t TestInfo) TestName() string {
	return t.testName
}

// FileSets returns the filesets for each stage.
func (t TestInfo) FileSets() []*FileSet {
	return t.files
}

// LoadInputFile returns the input yaml file for this test.
func (f FileSet) LoadInputFile() ([]byte, error) {
	return Asset(f.inputFile)
}

// LoadExpectedFile returns the expected file for this test.
func (f FileSet) LoadExpectedFile() ([]byte, error) {
	return Asset(f.expectedFile)
}

// LoadExpectedResources loads and parses the expected resources from the expected file.
func (f FileSet) LoadExpectedResources(namespace string) (map[string][]map[string]interface{}, error) {
	b, err := f.LoadExpectedFile()
	if err != nil {
		return nil, err
	}

	s, err := applyNamespace(string(b), namespace)
	if err != nil {
		return nil, err
	}

	expectedRecord := make(map[string]interface{})
	if err := json.Unmarshal([]byte(s), &expectedRecord); err != nil {
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
func (f FileSet) HasMeshConfigFile() bool {
	return f.meshConfigFile != ""
}

// LoadMeshConfigFile returns the meshconfigfile for this test.
func (f FileSet) LoadMeshConfigFile() ([]byte, error) {
	return Asset(f.meshConfigFile)
}

func applyNamespace(txt, ns string) (string, error) {
	t := template.New("ns")
	t, err := t.Parse(txt)
	if err != nil {
		return "", err
	}

	m := make(map[string]interface{})
	m["Namespace"] = ns

	var buf bytes.Buffer
	if err = t.Execute(&buf, m); err != nil {
		return "", err
	}

	return buf.String(), nil
}
