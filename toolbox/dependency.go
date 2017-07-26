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

package main

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

type dependency struct {
	name       string
	repoName   string
	prodBranch string // either master or stable
	file       string // where in the *parent* repo such dependecy is recorded
}

var (
	deps = make(map[string][]dependency)
)
