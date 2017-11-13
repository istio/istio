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
	"fmt"
	"log"
	"os"

	"path/filepath"

	"istio.io/istio/tests/integration/framework"
	"istio.io/istio/tests/util"
)

// LocalComponent is a component of local mixs binary in process
type LocalComponent struct {
	framework.Component
	name      string
	process   *os.Process
	logFile   string
	configDir string
}

// NewLocalComponent create a LocalComponent with name, log dir and config dir
func NewLocalComponent(n, logDir, configDir string) *LocalComponent {
	logFile := fmt.Sprintf("%s/%s.log", logDir, n)
	return &LocalComponent{
		name:      n,
		logFile:   logFile,
		configDir: configDir,
	}
}

// GetName return component name
func (mixerComp *LocalComponent) GetName() string {
	return mixerComp.name
}

// Start brings up a local mixs using test config files in local file system
func (mixerComp *LocalComponent) Start() (err error) {
	if _, err = util.Shell("bazel build -c opt mixer/cmd/server:mixs"); err != nil {
		log.Printf("Failed to build misx: %s", err)
		return err
	}
	emptyDir := filepath.Join(mixerComp.configDir, "emptydir")
	if _, err = util.Shell(fmt.Sprintf("mkdir -p %s", emptyDir)); err != nil {
		log.Printf("Failed to create emptydir: %v", err)
		return err
	}
	mixerConfig := filepath.Join(mixerComp.configDir, "mixerconfig")
	if _, err = util.Shell(fmt.Sprintf("mkdir -p %s", mixerConfig)); err != nil {
		log.Printf("Failed to create mixerconfig dir: %v", err)
		return err
	}
	if _, err = util.Shell("cp mixer/testdata/config/* %s", mixerConfig); err != nil {
		log.Printf("Failed to copy config for test: %v", err)
		return err
	}
	if err = os.Remove(filepath.Join(mixerConfig, "stackdriver.yaml")); err != nil {
		log.Printf("Failed to remove stackdriver.yaml: %v", err)
	}

	mixerComp.process, err = util.RunBackground(fmt.Sprintf("./bazel-bin/mixer/cmd/server/mixs server"+
		" --configStore2URL=fs://%s --configStoreURL=fs://%s", mixerConfig, emptyDir))
	if err != nil {
		log.Printf("Failed to start component %s", mixerComp.GetName())
		return err
	}
	return
}

// Stop kill the mixer server process
func (mixerComp *LocalComponent) Stop() (err error) {
	err = util.KillProcess(mixerComp.process)
	if err != nil {
		log.Printf("Failed to Stop component %s", mixerComp.GetName())
	}
	return
}

// IsAlive check the process of local server is running
// TODO: Process running doesn't guarantee server is ready
// TODO: Need a better way to check if component is alive/running
func (mixerComp *LocalComponent) IsAlive() (bool, error) {
	return util.IsProcessRunning(mixerComp.process)
}

// Cleanup clean up tmp files and other resource created by LocalComponent
func (mixerComp *LocalComponent) Cleanup() error {
	return nil
}
