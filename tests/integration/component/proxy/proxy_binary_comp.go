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
	"net"
	"sync"
	"time"

	"istio.io/istio/tests/integration/framework"
)

const (
	sideCarEndpoint = "http://localhost:9090/echo"
	sideCarPort     = "localhost:9090"
)

var (
	envoyStartScript = flag.String("envoy_start_script", "", "start_envoy script")
	envoyBinary      = flag.String("envoy_binary", "", "Envoy binary path.")
	lock             sync.Mutex
)

// LocalCompConfig contains configs for LocalComponent
type LocalCompConfig struct {
	framework.Config
	LogFile string
}

// LocalCompStatus contains status for LocalComponent
type LocalCompStatus struct {
	framework.Status
	SideCarEndpoint string
}

// LocalComponent is a component of local proxy binary in process
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
func (proxyComp *LocalComponent) GetName() string {
	return proxyComp.name
}

// GetConfig return the config for outside use
func (proxyComp *LocalComponent) GetConfig() framework.Config {
	lock.Lock()
	config := proxyComp.config
	lock.Unlock()
	return config
}

// SetConfig set a config into this component
func (proxyComp *LocalComponent) SetConfig(config framework.Config) error {
	proxyConfig, ok := config.(LocalCompConfig)
	if !ok {
		return fmt.Errorf("cannot cast config into proxy local config")
	}
	lock.Lock()
	proxyComp.config = proxyConfig
	lock.Unlock()
	return nil
}

// GetStatus return the status for outside use
func (proxyComp *LocalComponent) GetStatus() framework.Status {
	lock.Lock()
	status := proxyComp.status
	lock.Unlock()
	return status
}

// Start brings up a local envoy using start_envory script from istio/proxy
func (proxyComp *LocalComponent) Start() (err error) {
	if err = proxyComp.testProcess.Start(fmt.Sprintf("%s -e %s > %s 2>&1",
		*envoyStartScript, *envoyBinary, proxyComp.config.LogFile)); err != nil {
		return
	}

	lock.Lock()
	proxyComp.status.SideCarEndpoint = sideCarEndpoint
	lock.Unlock()

	// TODO: Find more reliable way to tell if local components are ready to serve
	time.Sleep(3 * time.Second)

	log.Printf("Started component %s", proxyComp.GetName())
	return
}

// IsAlive check the process of local proxy is listening on port.
func (proxyComp *LocalComponent) IsAlive() (bool, error) {
	isRunning, err := proxyComp.testProcess.IsRunning()
	if !isRunning || err != nil {
		return isRunning, err
	}
	_, err = net.Dial("tcp", sideCarPort)
	if err == nil {
		return true, err
	}
	return false, err
}

// Stop stop this local component by kill the process
func (proxyComp *LocalComponent) Stop() (err error) {
	log.Printf("Stopping component %s", proxyComp.GetName())
	return proxyComp.testProcess.Stop()
}
