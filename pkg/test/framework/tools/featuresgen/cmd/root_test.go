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

package cmd

import (
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestCreateConstantString(t *testing.T) {
	s1 := createConstantString([]string{"lab-el1", "label2"})
	if s1 != "\tLabEl1_Label2\tFeature = \"lab-el1.label2\"" {
		t.Errorf("Expected '\tLabEl1_Label2\tFeature = \"lab-el1.label2\"' got '%s'", s1)
	}

	s2 := createConstantString([]string{"lab.*)($)#el1", "lab.el2"})
	if s2 != "\tLabel1_Label2\tFeature = \"lab*)($)#el1.label2\"" {
		t.Errorf("Expected '\tLabel1_Label2\tFeature = \"lab*)($)#el1.label2\"' got '%s'", s2)
	}
}

var testYaml = `
features:
  values: [hello1, hello2]
  key2:
    values: [val1, val2]
`

const expectedResult = `	Hello1	Feature = "hello1"
	Hello2	Feature = "hello2"
	Key2_Val1	Feature = "key2.val1"
	Key2_Val2	Feature = "key2.val2"`

func TestReadVal(t *testing.T) {
	m := make(map[any]any)

	err := yaml.Unmarshal([]byte(testYaml), &m)
	if err != nil {
		t.Errorf(err.Error())
	}

	s1 := readVal(m, make([]string, 0))

	sort.Strings(s1)

	if strings.Join(s1, "\n") != expectedResult {
		t.Errorf("Expected '%s' got '%s'", expectedResult, strings.Join(s1, "\n"))
	}
}
