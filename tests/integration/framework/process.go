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

type Process struct {
	Process *os.Process
}

func (p *Process) Start(command string) (err error) {
	p.Process, err = util.RunBackground(command)
	return
}

func (p *Process) Stop() (err error) {
	err = util.KillProcess(p.Process)
	return
}
