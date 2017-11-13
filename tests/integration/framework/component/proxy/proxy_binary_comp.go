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
	"fmt"
	"log"
	"os"

	"istio.io/istio/tests/integration/framework"
	"istio.io/istio/tests/util"
)

const (
	proxyRepo = "../proxy"
)

// LocalComponent is a component of local proxy binary in process
type LocalComponent struct {
	framework.Component
	name    string
	process *os.Process
	logFile string
}

// NewLocalComponent create a LocalComponent with name and log dir
func NewLocalComponent(n, logDir string) *LocalComponent {
	logFile := fmt.Sprintf("%s/%s.log", logDir, n)
	return &LocalComponent{
		name:    n,
		logFile: logFile,
	}
}

// GetName return component name
func (proxyComp *LocalComponent) GetName() string {
	return proxyComp.name
}

// Start brings up a local envoy using start_envory script from istio/proxy
func (proxyComp *LocalComponent) Start() (err error) {
	if _, err = util.Shell("bazel build -c opt %s/src/envoy/mixer:envoy", proxyRepo); err != nil {
		log.Printf("Failed to build envoy from proxy repo")
		return err
	}
	if proxyComp.process, err = util.RunBackground(fmt.Sprintf("./%s/src/envoy/mixer/start_envoy > %s 2>&1",
		proxyRepo, proxyComp.logFile)); err != nil {
		log.Printf("Failed to start component %s", proxyComp.GetName())
		return err
	}
	return
}

// Stop kill the envory process
func (proxyComp *LocalComponent) Stop() (err error) {
	err = util.KillProcess(proxyComp.process)
	if err != nil {
		log.Printf("Failed to Stop component %s", proxyComp.GetName())
	}
	return
}

// IsAlive check the process of local server is running
// TODO: Process running doesn't guarantee server is ready
// TODO: Need a better way to check if component is alive/running
func (proxyComp *LocalComponent) IsAlive() (bool, error) {
	return util.IsProcessRunning(proxyComp.process)
}

// Cleanup clean up tmp files and other resource created by LocalComponent
func (proxyComp *LocalComponent) Cleanup() error {
	return nil
}
