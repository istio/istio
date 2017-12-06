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
	"flag"
	"fmt"
	"log"
	"time"

	"istio.io/istio/tests/integration/framework"
)

var (
	fortioBinary = flag.String("fortio_binary", "", "Fortio binary path.")
)

// LocalComponent is a local fortio server componment
type LocalComponent struct {
	framework.Component
	testProcess framework.TestProcess
	Name        string
	LogFile     string
}

// NewLocalComponent create a LocalComponent with name and log dir
func NewLocalComponent(n, logDir string) *LocalComponent {
	logFile := fmt.Sprintf("%s/%s.log", logDir, n)
	return &LocalComponent{
		Name:    n,
		LogFile: logFile,
	}
}

// GetName implement the function in component interface
func (fortioServerComp *LocalComponent) GetName() string {
	return fortioServerComp.Name
}

// Start brings up a local fortio echo server
func (fortioServerComp *LocalComponent) Start() (err error) {
	if err = fortioServerComp.testProcess.Start(fmt.Sprintf("%s server > %s 2>&1",
		*fortioBinary, fortioServerComp.LogFile)); err != nil {
		return
	}

	// TODO: Find more reliable way to tell if local components are ready to serve
	time.Sleep(2 * time.Second)
	log.Printf("Started component %s", fortioServerComp.GetName())
	return
}

// IsAlive check the process of local server is running
// TODO: Process running doesn't guarantee server is ready
// TODO: Need a better way to check if component is alive/running
func (fortioServerComp *LocalComponent) IsAlive() (bool, error) {
	return fortioServerComp.testProcess.IsRunning()
}

// Stop stop this local component by kill the process
func (fortioServerComp *LocalComponent) Stop() (err error) {
	log.Printf("Stopping component %s", fortioServerComp.GetName())
	return fortioServerComp.testProcess.Stop()
}

// Cleanup clean up tmp files and other resource created by LocalComponent
func (fortioServerComp *LocalComponent) Cleanup() error {
	return nil
}
