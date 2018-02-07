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

	mwi "istio.io/istio/security/cmd/node_agent_k8s/mgmtwlhintf"
	nam "istio.io/istio/security/cmd/node_agent_k8s/nodeagentmgmt"
	wlapi "istio.io/istio/security/cmd/node_agent_k8s/workloadapi"
	wlh "istio.io/istio/security/cmd/node_agent_k8s/workloadhandler"
)

const (
	// MgmtAPIPath is the path to call mgmt
	MgmtAPIPath string = "/tmp/udsuspver/mgmt.sock"

	// WorkloadAPIUdsHome is the path for workload
	WorkloadAPIUdsHome string = "/tmp/nodeagent"
)

var (
	// CfgMgmtAPIPath is the path for management api
	CfgMgmtAPIPath string

	// CfgWldAPIUdsHome is the path for worklaod api
	CfgWldAPIUdsHome string

	// RootCmd define the command for node agent
	RootCmd = &cobra.Command{
		Use:   "nodeagent",
		Short: "Node agent",
		Long:  "Node agent with both mgmt and workload api interfaces.",
	}
)

func init() {
	RootCmd.PersistentFlags().StringVarP(&CfgMgmtAPIPath, "mgmtpath", "m", MgmtAPIPath, "Mgmt API Uds path")
	RootCmd.PersistentFlags().StringVarP(&CfgWldAPIUdsHome, "wldpath", "w", WorkloadAPIUdsHome, "Workload API home path")
}

// MgmtAPI manage the api
func MgmtAPI() {
	// initialize the workload api.
	wl := wlapi.NewWlAPIServer()
	// initialize the workload api handler with the workload api.
	wli := mwi.NewWlHandler(wl, wlh.NewServer)
	// finally initialize the node mgmt interface with workload handler.
	mgmtServer := nam.NewServer(CfgWldAPIUdsHome, wli)

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func(s *nam.Server, c chan os.Signal) {
		<-c
		s.Stop()
		s.WaitDone()
		os.Exit(1)
	}(mgmtServer, sigc)

	mgmtServer.Serve(true, CfgMgmtAPIPath)
}

func main() {
	if err := RootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
	// Check if the base directory exisits
	_, e := os.Stat(WorkloadAPIUdsHome)
	if e != nil {
		log.Fatalf("workloadApi directory not present (%v)", WorkloadAPIUdsHome)
	}
	MgmtAPI()
}
