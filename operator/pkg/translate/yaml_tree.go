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

package translate

import (
	"gopkg.in/yaml.v2"

	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
)

// YAMLTree takes an input tree inTreeStr, a partially constructed output tree outTreeStr, and a map of
// translations of source-path:dest-path in pkg/tpath format. It returns an output tree with paths from the input
// tree, translated and overlaid on the output tree.
func YAMLTree(inTreeStr, outTreeStr string, translations map[string]string) (string, error) {
	inTree := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(inTreeStr), &inTree); err != nil {
		return "", err
	}
	outTree := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(outTreeStr), &outTree); err != nil {
		return "", err
	}

	for inPath, translation := range translations {
		path := util.PathFromString(inPath)
		node, found, err := tpath.Find(inTree, path)
		if err != nil {
			return "", err
		}
		if !found {
			continue
		}

		if err := tpath.MergeNode(outTree, util.PathFromString(translation), node); err != nil {
			return "", err
		}
	}

	outYAML, err := yaml.Marshal(outTree)
	if err != nil {
		return "", err
	}
	return string(outYAML), nil
}
