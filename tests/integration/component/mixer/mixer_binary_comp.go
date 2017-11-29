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
	"time"

	"istio.io/istio/tests/integration/component"
	"istio.io/istio/tests/util"
)

const(
	testConfigPath = "mixer/testdata/config"
)

// LocalComponent is a component of local mixs binary in process
type LocalComponent struct {
	component.CommonProcesssComp
	configDir string
}

// NewLocalComponent create a LocalComponent with name, log dir and config dir
func NewLocalComponent(n, binaryPath, logDir, configDir string) *LocalComponent {
	logFile := fmt.Sprintf("%s/%s.log", logDir, n)
	return &LocalComponent{
		CommonProcesssComp: component.CommonProcesssComp{
			CommonComp: component.CommonComp{
				Name:    n,
				LogFile: logFile,
			},
			BinaryPath: binaryPath,
		},
		configDir: configDir,
	}
}

// Start brings up a local mixs using test config files in local file system
func (mixerComp *LocalComponent) Start() (err error) {
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
	mixerTestConfig := util.GetResourcePath(testConfigPath)
	if _, err = util.Shell("cp %s/* %s", mixerTestConfig, mixerConfig); err != nil {
		log.Printf("Failed to copy config for test: %v", err)
		return err
	}
	if err = os.Remove(filepath.Join(mixerConfig, "stackdriver.yaml")); err != nil {
		log.Printf("Failed to remove stackdriver.yaml: %v", err)
	}

	mixerComp.Process, err = util.RunBackground(fmt.Sprintf("%s server"+
		" --configStore2URL=fs://%s --configStoreURL=fs://%s",
			"/Users/yutongz/go/src/istio.io/istio/bazel-bin/mixer/cmd/server/mixs", mixerConfig, emptyDir))

	// TODO: Find more reliable way to tell if local components are ready to serve
	time.Sleep(3 * time.Second)
	return
}

// IsAlive check the process of local server is running
// TODO: Process running doesn't guarantee server is ready
// TODO: Need a better way to check if component is alive/running
func (mixerComp *LocalComponent) IsAlive() (bool, error) {
	return util.IsProcessRunning(mixerComp.Process)
}

// Cleanup clean up tmp files and other resource created by LocalComponent
func (mixerComp *LocalComponent) Cleanup() error {
	return nil
}
