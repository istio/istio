// Copyright 2017 Istio Authors
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

package util

import (
	"encoding/json"
	"io/ioutil"
)

/*
	Assumes Dependency Hoisting in all bazel dependency files. Example:

		new_git_repository(
			name = "mixerapi_git",
			commit = "ee9769f5b3304d9e01cd7ed6fb1dbb9b08e96210",
			remote = "https://github.com/istio/api.git",
		)

	becomes

		MIXERAPI = "ee9769f5b3304d9e01cd7ed6fb1dbb9b08e96210"
		new_git_repository(
			name = "mixerapi_git",
			commit = MIXERAPI,
			remote = "https://github.com/istio/api.git",
		)
*/

// Dependency records meta data
type Dependency struct {
	Comment       string `json:"_comment"` // hack comment into json file
	Name          string `json:"name"`
	RepoName      string `json:"repoName"`
	File          string `json:"file"`          // where in the *parent* repo such dependecy is recorded
	LastStableSHA string `json:"lastStableSHA"` // sha used in the latest stable build of parent
}

// DeserializeDeps get the list of dependencies of a repo by
// deserializing the file on depsFilePath
func DeserializeDeps(depsFilePath string) ([]Dependency, error) {
	var deps []Dependency
	raw, err := ioutil.ReadFile(depsFilePath)
	if err != nil {
		return deps, err
	}
	err = json.Unmarshal(raw, &deps)
	return deps, err
}

// DeserializeDepsFromString inflates the list of dependencies from a string
func DeserializeDepsFromString(pickled string) ([]Dependency, error) {
	var deps []Dependency
	err := json.Unmarshal([]byte(pickled), &deps)
	return deps, err
}

// SerializeDeps writes in-memory dependencies to file
func SerializeDeps(depsFilePath string, deps *[]Dependency) error {
	pickled, err := json.MarshalIndent(*deps, "", "\t")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(depsFilePath, pickled, 0600)
}
