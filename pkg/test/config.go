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

package test

import (
	"io/ioutil"
	"strings"
)

const separator = "\n---\n"

// JoinConfigs merges the given config snippets together
func JoinConfigs(parts ...string) string {
	// remove empty strings
	var tmp []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			tmp = append(tmp, p)
		}
	}
	return strings.Join(tmp, separator)
}

// SplitConfigs splits config into chunks, based on the "---" separator.
func SplitConfigs(cfg string) []string {
	return strings.Split(cfg, separator)
}

func ReadConfigFile(filepath string) (string, error) {
	by, err := ioutil.ReadFile(filepath)
	if err != nil {
		return "", err
	}

	return string(by), nil
}
