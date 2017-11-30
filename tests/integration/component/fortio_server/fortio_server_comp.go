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
	"time"

	"istio.io/istio/tests/integration/framework"
	"istio.io/istio/tests/util"
	"log"
)

var (
	fortioBinary = flag.String("fortio_binary", "", "Fortio binary path.")
)

// LocalComponent is a local fortio server componment
type LocalComponent struct {
	framework.CommonProcessComp
}

// NewLocalComponent create a LocalComponent with name and log dir
func NewLocalComponent(n, logDir string) *LocalComponent {
	logFile := fmt.Sprintf("%s/%s.log", logDir, n)

	return &LocalComponent{
		CommonProcessComp: framework.CommonProcessComp{
			CommonComp: framework.CommonComp{
				Name:    n,
				LogFile: logFile,
			},
			BinaryPath: *fortioBinary,
		},
	}
}

// Start brings up a local fortio echo server
func (FortioServerComp *LocalComponent) Start() (err error) {
	FortioServerComp.Process, err = util.RunBackground(fmt.Sprintf("%s server > %s 2>&1 &",
		FortioServerComp.BinaryPath, FortioServerComp.LogFile))

	// TODO: Find more reliable way to tell if local components are ready to serve
	time.Sleep(2 * time.Second)
	log.Printf("Started component %s", FortioServerComp.GetName())
	return
}

// IsAlive check the process of local server is running
// TODO: Process running doesn't guarantee server is ready
// TODO: Need a better way to check if component is alive/running
func (FortioServerComp *LocalComponent) IsAlive() (bool, error) {
	return util.IsProcessRunning(FortioServerComp.Process)
}

// Cleanup clean up tmp files and other resource created by LocalComponent
func (FortioServerComp *LocalComponent) Cleanup() error {
	return nil
}
