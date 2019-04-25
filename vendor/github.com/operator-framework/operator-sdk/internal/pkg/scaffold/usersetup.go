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

package scaffold

import (
	"path/filepath"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

const UserSetupFile = "user_setup"

// UserSetup - userSetup script
type UserSetup struct {
	input.Input
}

func (u *UserSetup) GetInput() (input.Input, error) {
	if u.Path == "" {
		u.Path = filepath.Join(BuildScriptDir, UserSetupFile)
	}
	u.TemplateBody = userSetupTmpl
	u.IsExec = true
	return u.Input, nil
}

const userSetupTmpl = `#!/bin/sh
set -x

# ensure $HOME exists and is accessible by group 0 (we don't know what the runtime UID will be)
mkdir -p ${HOME}
chown ${USER_UID}:0 ${HOME}
chmod ug+rwx ${HOME}

# runtime user will need to be able to self-insert in /etc/passwd
chmod g+rw /etc/passwd

# no need for this script to remain in the image after running
rm $0
`
