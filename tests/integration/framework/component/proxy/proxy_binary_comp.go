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

type ProxyComponent struct {
	framework.Component
	name    string
	process *os.Process
	logFile string
}

func NewProxyComponent(n, logDir string) *ProxyComponent {
	logFile := fmt.Sprintf("%s/%s.log", logDir, n)
	return &ProxyComponent{
		name:    n,
		logFile: logFile,
	}
}

func (proxyComp *ProxyComponent) GetName() string {
	return proxyComp.name
}

func (proxyComp *ProxyComponent) Start() (err error) {
	_, err = util.Shell("bazel build -c opt %s/src/envoy/mixer:envoy", proxyRepo)
	proxyComp.process, err = util.RunBackground(fmt.Sprintf("./%s/src/envoy/mixer/start_envoy > %s 2>&1", proxyRepo, proxyComp.logFile))
	if err != nil {
		log.Printf("Failed to start component %s", proxyComp.GetName())
		return err
	}
	return
}

func (proxyComp *ProxyComponent) Stop() (err error) {
	err = util.KillProcess(proxyComp.process)
	if err != nil {
		log.Printf("Failed to Stop component %s", proxyComp.GetName())
	}
	return
}

func (proxyComp *ProxyComponent) IsAlive() (bool, error) {
	return util.IsProcessRunning(proxyComp.process)
}

func (proxyComp *ProxyComponent) Cleanup() error {
	return nil
}
