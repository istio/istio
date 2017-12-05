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

package proxy

import (
	"flag"
	"fmt"
	"log"
	"time"

	"istio.io/istio/tests/integration/framework"
)

var (
	envoyBinary = flag.String("envoy_binary", "", "Envoy binary path.")
)

// LocalComponent is a component of local proxy binary in process
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
func (proxyComp *LocalComponent) GetName() string {
	return proxyComp.Name
}

// Start brings up a local envoy using start_envory script from istio/proxy
func (proxyComp *LocalComponent) Start() (err error) {
	if err = proxyComp.testProcess.Start(fmt.Sprintf("%s > %s 2>&1",
		*envoyBinary, proxyComp.LogFile)); err != nil {
		return
	}

	// TODO: Find more reliable way to tell if local components are ready to serve
	time.Sleep(3 * time.Second)

	log.Printf("Started component %s", proxyComp.GetName())
	return
}

// IsAlive check the process of local server is running
// TODO: Process running doesn't guarantee server is ready
// TODO: Need a better way to check if component is alive/running
func (proxyComp *LocalComponent) IsAlive() (bool, error) {
	return proxyComp.testProcess.IsRunning()
}

// Stop stop this local component by kill the process
func (proxyComp *LocalComponent) Stop() (err error) {
	log.Printf("Stopping component %s", proxyComp.GetName())
	return proxyComp.testProcess.Stop()
}

// Cleanup clean up tmp files and other resource created by LocalComponent
func (proxyComp *LocalComponent) Cleanup() error {
	return nil
}
