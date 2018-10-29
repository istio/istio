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

package framework

import (
	"os"

	"istio.io/istio/tests/util"
)

// TestProcess is a wrap of os.Process
// With implemented methods to control local components
type TestProcess struct {
	Process *os.Process
}

// Start starts a background process with given command
func (tp *TestProcess) Start(command string) (err error) {
	tp.Process, err = util.RunBackground(command)
	return
}

// Stop kills the process
func (tp *TestProcess) Stop() (err error) {
	err = util.KillProcess(tp.Process)
	return
}

// IsRunning checks if the process is still running
func (tp *TestProcess) IsRunning() (running bool, err error) {
	return util.IsProcessRunning(tp.Process)
}
