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

package mixer

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"istio.io/istio/tests/integration/framework"
	"istio.io/istio/tests/util"
)

const (
	testConfigPath  = "mixer/testdata/config"
	metricsEndpoint = "http://localhost:42422/metrics"
)

var (
	mixerBinary = flag.String("mixer_binary", "", "Mixer binary path.")
	lock        sync.Mutex
)

// LocalCompConfig contains configs for LocalComponent
type LocalCompConfig struct {
	framework.Config
	ConfigFileDir string
	LogFile       string
}

// LocalCompStatus contains status for LocalComponent
type LocalCompStatus struct {
	framework.Status
	MetricsEndpoint string
}

// LocalComponent is a component of local mixs binary in process
type LocalComponent struct {
	framework.Component
	testProcess framework.TestProcess
	config      LocalCompConfig
	status      LocalCompStatus
	name        string
}

// NewLocalComponent create a LocalComponent with name, log dir and config dir
func NewLocalComponent(n string, config LocalCompConfig) *LocalComponent {
	return &LocalComponent{
		name:   n,
		config: config,
	}
}

// GetName implement the function in component interface
func (mixerComp *LocalComponent) GetName() string {
	return mixerComp.name
}

// GetConfig return the config for outside use
func (mixerComp *LocalComponent) GetConfig() framework.Config {
	lock.Lock()
	config := mixerComp.config
	lock.Unlock()
	return config
}

// SetConfig set a config into this component
func (mixerComp *LocalComponent) SetConfig(config framework.Config) error {
	mixerConfig, ok := config.(LocalCompConfig)
	if !ok {
		return fmt.Errorf("cannot cast config into mixer local config")
	}
	lock.Lock()
	mixerComp.config = mixerConfig
	lock.Unlock()
	return nil
}

// GetStatus return the status for outside use
func (mixerComp *LocalComponent) GetStatus() framework.Status {
	lock.Lock()
	status := mixerComp.status
	lock.Unlock()
	return status
}

// Start brings up a local mixs using test config files in local file system
func (mixerComp *LocalComponent) Start() (err error) {
	if err != nil {
		log.Printf("Failed to get current directory: %s", err)
		return
	}
	emptyDir := filepath.Join(mixerComp.config.ConfigFileDir, "emptydir")
	if _, err = util.Shell(fmt.Sprintf("mkdir -p %s", emptyDir)); err != nil {
		log.Printf("Failed to create emptydir: %v", err)
		return
	}
	mixerConfig := filepath.Join(mixerComp.config.ConfigFileDir, "mixerconfig")
	if _, err = util.Shell(fmt.Sprintf("mkdir -p %s", mixerConfig)); err != nil {
		log.Printf("Failed to create mixerconfig dir: %v", err)
		return
	}
	mixerTestConfig := util.GetResourcePath(testConfigPath)
	if _, err = util.Shell("cp %s/* %s", mixerTestConfig, mixerConfig); err != nil {
		log.Printf("Failed to copy config for test: %v", err)
		return
	}

	if err = mixerComp.testProcess.Start(fmt.Sprintf("%s server"+
		" --configStoreURL=fs://%s",
		*mixerBinary, mixerConfig)); err != nil {
		return
	}

	// TODO: Find more reliable way to tell if local components are ready to serve
	time.Sleep(5 * time.Second)

	lock.Lock()
	mixerComp.status.MetricsEndpoint = metricsEndpoint
	lock.Unlock()

	log.Printf("Started component %s", mixerComp.GetName())
	return
}

// IsAlive check the process of local server is running
// TODO: Process running doesn't guarantee server is ready
// TODO: Need a better way to check if component is alive/running
func (mixerComp *LocalComponent) IsAlive() (bool, error) {
	return mixerComp.testProcess.IsRunning()
}

// Stop stop this local component by kill the process
func (mixerComp *LocalComponent) Stop() (err error) {
	log.Printf("Stopping component %s", mixerComp.GetName())
	return mixerComp.testProcess.Stop()
}
