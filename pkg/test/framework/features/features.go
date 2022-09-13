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

package features

import (
	"fmt"
	"os"
	"strings"

	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/test/env"
	"istio.io/pkg/log"
)

type Feature string

// Checker ensures that the values passed to Features() are in the features.yaml file
type Checker interface {
	Check(feature Feature) (check bool, scenario string)
}

type checkerImpl struct {
	m map[string]any
}

func BuildChecker(yamlPath string) (Checker, error) {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		log.Errorf("Error reading feature file: %s", yamlPath)
		return nil, err
	}
	m := make(map[string]any)

	err = yaml.Unmarshal(data, &m)
	if err != nil {
		log.Errorf("Error parsing features file: %s", err)
		return nil, err
	}
	return &checkerImpl{m["features"].(map[string]any)}, nil
}

// returns true if the feature is defined in features.yaml,
// false if not
func (c *checkerImpl) Check(feature Feature) (check bool, scenario string) {
	return checkPathSegment(c.m, strings.Split(string(feature), "."))
}

func checkPathSegment(m map[string]any, path []string) (check bool, scenario string) {
	if len(path) < 1 {
		return false, ""
	}
	segment := path[0]
	if val, ok := m[segment]; ok {
		if valmap, ok := val.(map[string]any); ok {
			return checkPathSegment(valmap, path[1:])
		} else if val == nil {
			return true, strings.Join(path[1:], ".")
		}
	}
	return false, ""
}

var GlobalAllowlist = fromFile(env.IstioSrc + "/pkg/test/framework/features/allowlist.txt")

type Allowlist struct {
	hashset map[string]bool
}

func fromFile(path string) *Allowlist {
	result := &Allowlist{hashset: map[string]bool{}}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Errorf("Error reading allowlist file: %s", path)
		return nil
	}
	for _, i := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(i, "//") {
			continue
		}
		result.hashset[i] = true
	}
	return result
}

func (w *Allowlist) Contains(suite, test string) bool {
	_, ok := w.hashset[fmt.Sprintf("%s,%s", suite, test)]
	return ok
}
