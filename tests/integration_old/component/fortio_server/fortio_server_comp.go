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
	"sync"
	"time"

	"istio.io/istio/tests/integration_old/framework"
)

const (
	serverEndpoint = "http://localhost:8080/"
)

var (
	fortioBinary = flag.String("fortio_binary", "", "Fortio binary path.")
	lock         sync.Mutex
)

// LocalCompConfig contains configs for LocalComponent
type LocalCompConfig struct {
	framework.Config
	LogFile string
}

// LocalCompStatus contains status for LocalComponent
type LocalCompStatus struct {
	framework.Status
	EchoEndpoint string
}

// LocalComponent is a local fortio server componment
type LocalComponent struct {
	framework.Component
	testProcess framework.TestProcess
	name        string
	config      LocalCompConfig
	status      LocalCompStatus
}

// NewLocalComponent create a LocalComponent with name and log dir
func NewLocalComponent(n string, config LocalCompConfig) *LocalComponent {
	return &LocalComponent{
		name:   n,
		config: config,
	}
}

// GetName implement the function in component interface
func (fortioServerComp *LocalComponent) GetName() string {
	return fortioServerComp.name
}

// GetConfig return the config for outside use
func (fortioServerComp *LocalComponent) GetConfig() framework.Config {
	lock.Lock()
	config := fortioServerComp.config
	lock.Unlock()
	return config
}

// SetConfig set a config into this component
func (fortioServerComp *LocalComponent) SetConfig(config framework.Config) error {
	fortioConfig, ok := config.(LocalCompConfig)
	if !ok {
		return fmt.Errorf("cannot cast config into fortio local config")
	}
	lock.Lock()
	fortioServerComp.config = fortioConfig
	lock.Unlock()
	return nil
}

// GetStatus return the status for outside use
func (fortioServerComp *LocalComponent) GetStatus() framework.Status {
	lock.Lock()
	status := fortioServerComp.status
	lock.Unlock()
	return status
}

// Start brings up a local fortio echo server
func (fortioServerComp *LocalComponent) Start() (err error) {
	if err = fortioServerComp.testProcess.Start(fmt.Sprintf("%s server > %s 2>&1",
		*fortioBinary, fortioServerComp.config.LogFile)); err != nil {
		return
	}

	lock.Lock()
	fortioServerComp.status.EchoEndpoint = serverEndpoint
	lock.Unlock()

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
