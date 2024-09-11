// Copyright 2019 Istio Authors
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

package driver

import (
	"os"
	"path/filepath"

	// nolint: staticcheck
	legacyproto "github.com/golang/protobuf/proto"
	"sigs.k8s.io/yaml"

	testenv "istio.io/istio/pkg/test/env"
)

// Loads resources in the test data directory
// Functions here panic since test artifacts are usually loaded prior to execution,
// so there is no clean-up necessary.

// Normalizes test data path
func TestPath(testFileName string) string {
	return filepath.Join(testenv.IstioSrc, "/tests/envoye2e/", testFileName)
}

// Loads a test file content
func LoadTestData(testFileName string) string {
	data, err := os.ReadFile(TestPath(testFileName))
	if err != nil {
		panic(err)
	}
	return string(data)
}

// Load a YAML and converts to JSON
func LoadTestJSON(testFileName string) string {
	data := LoadTestData(testFileName)
	js, err := yaml.YAMLToJSON([]byte(data))
	if err != nil {
		panic(err)
	}
	return string(js)
}

// Loads a test file and fills in template variables
func (p *Params) LoadTestData(testFileName string) string {
	data := LoadTestData(testFileName)
	out, err := p.Fill(data)
	if err != nil {
		panic(err)
	}
	return out
}

// Fills in template variables in the given template data
func (p *Params) FillTestData(data string) string {
	out, err := p.Fill(data)
	if err != nil {
		panic(err)
	}
	return out
}

// Loads a test file as YAML into a proto and fills in template variables
func (p *Params) LoadTestProto(testFileName string, msg legacyproto.Message) legacyproto.Message {
	data := LoadTestData(testFileName)
	if err := p.FillYAML(data, msg); err != nil {
		panic(err)
	}
	return msg
}
