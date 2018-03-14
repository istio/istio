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
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	nam "istio.io/istio/security/cmd/node_agent_k8s/nodeagentmgmt"
	"istio.io/istio/security/cmd/node_agent_k8s/workload/handler"
	wlapi "istio.io/istio/security/cmd/node_agent_k8s/workloadapi"
)

const (
	// MgmtAPIPath is the path to call mgmt.
	MgmtAPIPath string = "/tmp/udsuspver/mgmt.sock"

	// WorkloadAPIUdsHome is the uds file directory for workload api.
	WorkloadAPIUdsHome string = "/tmp/nodeagent"

	// WorkloadAPIUdsFile is the uds file name for workload api.
	WorkloadAPIUdsFile string = "/server.sock"
)

var (
	// CfgMgmtAPIPath is the path for management api.
	CfgMgmtAPIPath string

	// CfgWldAPIUdsHome is the home directory for workload api.
	CfgWldAPIUdsHome string

	// CfgWldSockFile is the file name for workload api.
	CfgWldSockFile string

	// RootCmd defines the command for node agent.
	RootCmd = &cobra.Command{
		Use:   "nodeagent",
		Short: "Node agent",
		Long:  "Node agent with both mgmt and workload api interfaces.",
	}
)

func init() {
	RootCmd.PersistentFlags().StringVar(&CfgMgmtAPIPath, "mgmtpath", MgmtAPIPath, "Mgmt API Uds path")
	RootCmd.PersistentFlags().StringVar(&CfgWldAPIUdsHome, "wldpath", WorkloadAPIUdsHome, "Workload API home path")
	RootCmd.PersistentFlags().StringVar(&CfgWldSockFile, "wldfile", WorkloadAPIUdsFile, "Workload API socket file name")
}

// MgmtAPI manage the api
func MgmtAPI() {
	mgmtServer := nam.NewServer(nam.ServerOptions{
		WorkloadOpts: handler.Options{PathPrefix: CfgWldAPIUdsHome, SockFile: CfgWldSockFile, RegAPI: wlapi.RegisterGrpc},
	})
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func(s *nam.Server, c chan os.Signal) {
		<-c
		s.Stop()
		s.WaitDone()
		os.Exit(1)
	}(mgmtServer, sigc)

	mgmtServer.Serve(CfgMgmtAPIPath)
}

func main() {
	if err := RootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
	// Check if the base directory exists.
	_, e := os.Stat(WorkloadAPIUdsHome)
	if e != nil {
		log.Fatalf("workloadApi directory not present (%v)", WorkloadAPIUdsHome)
	}
	MgmtAPI()
}
