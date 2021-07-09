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

// nolint: golint
package fuzz

import (
	"bytes"
	"errors"
	"strings"

	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube/inject"
)

var requiredJSONFields = []string{
	"policy", "defaultTemplates",
	"aliases", "neverInjectSelector",
	"alwaysInjectSelector",
	"injectedAnnotations",
}

func createStringMap(f *fuzz.ConsumeFuzzer) (map[string]string, error) {
	m := make(map[string]string)
	maxNoEntries := 50
	qty, err := f.GetInt()
	if err != nil {
		return m, err
	}
	noOfEntries := qty % maxNoEntries
	if noOfEntries == 0 {
		return m, errors.New("a map of zero-length was created")
	}
	for i := 0; i < noOfEntries; i++ {
		k, err := f.GetString()
		if err != nil {
			return m, err
		}
		v, err := f.GetString()
		if err != nil {
			return m, err
		}
		m[k] = v
	}
	return m, nil
}

func FuzzIntoResourceFile(data []byte) int {
	f := fuzz.NewConsumer(data)
	configData, err := f.GetBytes()
	if err != nil {
		return -1
	}
	for _, field := range requiredJSONFields {
		if !strings.Contains(string(configData), field) {
			return -1
		}
	}
	c, err := inject.UnmarshalConfig(configData)
	if err != nil {
		return 0
	}
	if len(c.Templates) == 0 {
		m, err := createStringMap(f)
		if err != nil {
			return 0
		}
		c.Templates = m
	}
	valuesConfig, err := f.GetString()
	if err != nil {
		return 0
	}
	meshYaml, err := f.GetString()
	if err != nil {
		return 0
	}
	mc, err := mesh.ApplyMeshConfigDefaults(meshYaml)
	if err != nil {
		return 0
	}
	inData, err := f.GetBytes()
	if err != nil {
		return 0
	}
	in := bytes.NewReader(inData)
	var got bytes.Buffer
	warn := func(s string) {}
	_ = inject.IntoResourceFile(nil, c.Templates, valuesConfig, "", mc, in, &got, warn)
	return 1
}
