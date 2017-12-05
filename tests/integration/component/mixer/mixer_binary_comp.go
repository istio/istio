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
	"os"
	"path/filepath"
	"time"

	"istio.io/istio/tests/integration/framework"
	"istio.io/istio/tests/util"
)

const (
	testConfigPath = "mixer/testdata/config"
)

var (
	mixerBinary = flag.String("mixer_binary", "", "Mixer binary path.")
)

// LocalComponent is a component of local mixs binary in process
type LocalComponent struct {
	framework.Component
	testProcess framework.TestProcess
	Name        string
	LogFile     string
	configDir   string
}

// NewLocalComponent create a LocalComponent with name, log dir and config dir
func NewLocalComponent(n, logDir, configDir string) *LocalComponent {
	logFile := fmt.Sprintf("%s/%s.log", logDir, n)
	return &LocalComponent{
		Name:      n,
		LogFile:   logFile,
		configDir: configDir,
	}
}

// GetName implement the function in component interface
func (mixerComp *LocalComponent) GetName() string {
	return mixerComp.Name
}

// Start brings up a local mixs using test config files in local file system
func (mixerComp *LocalComponent) Start() (err error) {
	wd, err := os.Getwd()
	if err != nil {
		log.Printf("Failed to get current directory: %s", err)
		return
	}
	emptyDir := filepath.Join(wd, mixerComp.configDir, "emptydir")
	if _, err = util.Shell(fmt.Sprintf("mkdir -p %s", emptyDir)); err != nil {
		log.Printf("Failed to create emptydir: %v", err)
		return
	}
	mixerConfig := filepath.Join(wd, mixerComp.configDir, "mixerconfig")
	if _, err = util.Shell(fmt.Sprintf("mkdir -p %s", mixerConfig)); err != nil {
		log.Printf("Failed to create mixerconfig dir: %v", err)
		return
	}
	mixerTestConfig := util.GetResourcePath(testConfigPath)
	if _, err = util.Shell("cp %s/* %s", mixerTestConfig, mixerConfig); err != nil {
		log.Printf("Failed to copy config for test: %v", err)
		return
	}
	if err = os.Remove(filepath.Join(mixerConfig, "stackdriver.yaml")); err != nil {
		log.Printf("Failed to remove stackdriver.yaml: %v", err)
		return
	}

	if err = mixerComp.testProcess.Start(fmt.Sprintf("%s server"+
		" --configStore2URL=fs://%s --configStoreURL=fs://%s",
		*mixerBinary, mixerConfig, emptyDir)); err != nil {
		return
	}

	// TODO: Find more reliable way to tell if local components are ready to serve
	time.Sleep(3 * time.Second)

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

// Cleanup clean up tmp files and other resource created by LocalComponent
func (mixerComp *LocalComponent) Cleanup() error {
	return nil
}
