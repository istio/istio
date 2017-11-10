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

	"istio.io/istio/tests/integration/framework"
	tu "istio.io/istio/tests/util"
)

type MixerComponent struct {
	framework.Component
	name    string
	process *os.Process
	logFile string
	configDir string
}

func NewMixerComponent(n, logDir, configDir string) *MixerComponent {
	logFile := fmt.Sprintf("%s/%s.log", logDir, n)
	return &MixerComponent{
		name:    n,
		logFile: logFile,
		configDir: configDir,
	}
}

func (mixerComp *MixerComponent) GetName() string {
	return mixerComp.name
}

func (mixerComp *MixerComponent) Start() (err error) {
	mixerComp.process, err = tu.RunBackground(fmt.Sprintf("./mixer/bazel-bin/cmd/server/mixs server" +
		" --configStore2URL=fs://%s/mixerconfig --configStoreURL=fs://%s/emptydir" +
			" --logtostderr > %s 2>&1", mixerComp.configDir, mixerComp.configDir, mixerComp.logFile))
	if err != nil {
		log.Printf("Failed to start component %s", mixerComp.GetName())
		return err
	}
	return
}

func (mixerComp *MixerComponent) Stop() (err error) {
	err = tu.KillProcess(mixerComp.process)
	if err != nil {
		log.Printf("Failed to Stop component %s", mixerComp.GetName())
	}
	return
}

func (mixerComp *MixerComponent)  IsAlive() (bool, error) {
	return tu.IsProcessRunning(mixerComp.process)
}

func (mixerComp *MixerComponent) Cleanup() error {
	return nil
}