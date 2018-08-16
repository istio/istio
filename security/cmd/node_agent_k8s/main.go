// Copyright 2018 Istio Authors
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

package main

import (
	"log"

	"github.com/spf13/cobra"
<<<<<<< HEAD

	nvm "istio.io/istio/security/pkg/nodeagent/vm"
)

const (
	// MgmtAPIPath is the path to call mgmt.
	MgmtAPIPath string = "/tmp/udsuspver/mgmt.sock"

	// WorkloadAPIUdsHome is the uds file directory for workload api.
	WorkloadAPIUdsHome string = "/tmp/nodeagent"

	// WorkloadAPIUdsFile is the uds file name for workload api.
	WorkloadAPIUdsFile string = "/server.sock"
=======
>>>>>>> 84e3ddf4c... Remove flexvolume driver, clean up node_agent_k8s dir (#6305)
)

var (
	// RootCmd defines the command for node agent.
	RootCmd = &cobra.Command{
		Use:   "nodeagent",
		Short: "Node agent",
	}
<<<<<<< HEAD

	naConfig nvm.Config
=======
>>>>>>> 84e3ddf4c... Remove flexvolume driver, clean up node_agent_k8s dir (#6305)
)

// Placeholder.
func startManagement() {
}

func main() {
	if err := RootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
	startManagement()
}
