// Copyright 2019 The Operator-SDK Authors
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

import (
	"path/filepath"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

//DockerfileHybrid - Dockerfile for a hybrid operator
type AoLogs struct {
	input.Input
}

// GetInput - gets the input
func (a *AoLogs) GetInput() (input.Input, error) {
	if a.Path == "" {
		a.Path = filepath.Join("bin", "ao-logs")
	}
	a.TemplateBody = aoLogsTmpl
	a.IsExec = true
	return a.Input, nil
}

const aoLogsTmpl = `#!/bin/bash

watch_dir=${1:-/tmp/ansible-operator/runner}
filename=${2:-stdout}
mkdir -p ${watch_dir}
inotifywait -r -m -e close_write ${watch_dir} | while read dir op file
do
  if [[ "${file}" = "${filename}" ]] ; then
    echo "${dir}/${file}"
    cat ${dir}/${file}
  fi
done
`
