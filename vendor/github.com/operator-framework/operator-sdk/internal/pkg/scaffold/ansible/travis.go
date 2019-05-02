// Copyright 2018 The Operator-SDK Authors
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

package ansible

import "github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"

const TravisFile = ".travis.yml"

type Travis struct {
	input.Input
}

// GetInput - gets the input
func (t *Travis) GetInput() (input.Input, error) {
	if t.Path == "" {
		t.Path = TravisFile
	}
	t.TemplateBody = travisAnsibleTmpl

	return t.Input, nil
}

const travisAnsibleTmpl = `sudo: required
services: docker
language: python
install:
  - pip install docker molecule openshift
script:
  - molecule test -s test-local
`
