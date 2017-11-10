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

type MixerComponent struct {
	framework.Component
	name      string
	process   *os.Process
	logFile   string
	configDir string
}

func NewMixerComponent(n, logDir, configDir string) *MixerComponent {
	logFile := fmt.Sprintf("%s/%s.log", logDir, n)
	return &MixerComponent{
		name:      n,
		logFile:   logFile,
		configDir: configDir,
	}
}

func (mixerComp *MixerComponent) GetName() string {
	return mixerComp.name
}

func (mixerComp *MixerComponent) Start() (err error) {
	if _, err = util.Shell("bazel build -c opt mixer/cmd/server:mixs"); err != nil {
		return err
	}
	emptyDir := filepath.Join(mixerComp.configDir, "emptydir")
	if err = os.Mkdir(emptyDir, os.ModeDir); err != nil {
		log.Printf("Failed to create emptydir: %v", err)
		return err
	}
	mixerConfig := filepath.Join(mixerComp.configDir, "mixerconfig")
	if err = os.Mkdir(mixerConfig, os.ModeDir); err != nil {
		log.Printf("Failed to create mixerconfig dir: %v", err)
		return err
	}
	if _, err = util.Shell("cp mixer/testdata/config/* %s", mixerConfig); err != nil {
		return err
	}
	if err = os.Remove(filepath.Join(mixerConfig, "stackdriver.yaml")); err != nil {
		log.Printf("Failed to remove stackdriver.yaml: %v", err)
	}
	if _, err = util.Shell("source mixer/bin/use_bazel_go.sh"); err != nil {
		return err
	}

	mixerComp.process, err = util.RunBackground(fmt.Sprintf("./mixer/bazel-bin/cmd/server/mixs server"+
		" --configStore2URL=fs://%s --configStoreURL=fs://%s > %s", mixerConfig, emptyDir, mixerComp.logFile))
	if err != nil {
		log.Printf("Failed to start component %s", mixerComp.GetName())
		return err
	}
	return
}

func (mixerComp *MixerComponent) Stop() (err error) {
	err = util.KillProcess(mixerComp.process)
	if err != nil {
		log.Printf("Failed to Stop component %s", mixerComp.GetName())
	}
	return
}

func (mixerComp *MixerComponent) IsAlive() (bool, error) {
	return util.IsProcessRunning(mixerComp.process)
}

func (mixerComp *MixerComponent) Cleanup() error {
	return nil
}
