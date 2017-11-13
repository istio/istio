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

package fortioServer

import (
	"fmt"
	"log"
	"os"

	"istio.io/istio/tests/integration/framework"
	"istio.io/istio/tests/util"
)

// LocalComponent is a local fortio server componment
type LocalComponent struct {
	framework.Component
	name    string
	process *os.Process
	logFile string
}

// NewLocalComponent create a LocalComponent with name and log dir
func NewLocalComponent(n, logDir string) *LocalComponent {
	logFile := fmt.Sprintf("%s/%s.log", logDir, n)
	return &LocalComponent{
		name:    n,
		logFile: logFile,
	}
}

// GetName return component name
func (FortioServerComp *LocalComponent) GetName() string {
	return FortioServerComp.name
}

// Start brings up a local fortio echo server
func (FortioServerComp *LocalComponent) Start() (err error) {
	if _, err = util.Shell("go get -u istio.io/fortio"); err != nil {
		log.Printf("Failed to go get fortio")
		return err
	}
	FortioServerComp.process, err = util.RunBackground(fmt.Sprintf("fortio server > %s 2>&1 &", FortioServerComp.logFile))
	if err != nil {
		log.Printf("Failed to start component %s", FortioServerComp.GetName())
		return err
	}
	return
}

// Stop kill the fortio server process
func (FortioServerComp *LocalComponent) Stop() (err error) {
	err = util.KillProcess(FortioServerComp.process)
	if err != nil {
		log.Printf("Failed to Stop component %s", FortioServerComp.GetName())
	}
	return
}

// IsAlive check the process of local server is running
// TODO: Process running doesn't guarantee server is ready
// TODO: Need a better way to check if component is alive/running
func (FortioServerComp *LocalComponent) IsAlive() (bool, error) {
	return util.IsProcessRunning(FortioServerComp.process)
}

// Cleanup clean up tmp files and other resource created by LocalComponent
func (FortioServerComp *LocalComponent) Cleanup() error {
	return nil
}
