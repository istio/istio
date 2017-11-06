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

package fortio_server

import (
	"fmt"
	"log"
	"os"

	"istio.io/istio/tests/integration/framework/component"
	tu "istio.io/istio/tests/util"
)

type FortioServerComp struct {
	component.Component
	name    string
	process *os.Process
	logFile string
}

func NewFortioServerComp(n, logDir string) *FortioServerComp {
	logFile := fmt.Sprintf("%s/%s.log", logDir, n)
	return &FortioServerComp{
		name:    n,
		logFile: logFile,
	}
}

func (FortioServerComp *FortioServerComp) GetName() string {
	return FortioServerComp.name
}

func (FortioServerComp *FortioServerComp) Start() (err error) {
	FortioServerComp.process, err = tu.RunBackground(fmt.Sprintf("fortio server > %s 2>&1 &", FortioServerComp.logFile))
	if err != nil {
		log.Printf("Failed to start component %s", FortioServerComp.GetName())
		return err
	}
	return
}

func (FortioServerComp *FortioServerComp) Stop() (err error) {
	err = tu.KillProcess(FortioServerComp.process)
	if err != nil {
		log.Printf("Failed to Stop component %s", FortioServerComp.GetName())
	}
	return
}

func (FortioServerComp *FortioServerComp) IsAlive() (bool, error) {
	return tu.IsProcessRunning(FortioServerComp.process)
}

func (FortioServerComp *FortioServerComp) Cleanup() error {
	return nil
}
