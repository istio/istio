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

	wlapi "istio.io/istio/security/cmd/node_agent_k8s/workloadapi"
	"istio.io/istio/security/pkg/flexvolume/binder"
	pb "istio.io/istio/security/proto"
)

const (
	// Node Agent directory where:
	// * Flexvolume posts credentials and
	// * Node Agent listens for workload's connecting to it
	// WorkloadAPIUdsHome is the path for workload
	WorkloadHome string = "/tmp/nodeagent"
)

var (

	// CfgWorkloadHome is the base bath where the workload credential and mounts are done.
	CfgWorkloadHome string

	// RootCmd define the command for node agent
	RootCmd = &cobra.Command{
		Use:   "nodeagent",
		Short: "Node agent",
		Long:  "Node agent with flex volume credential bootstrapping support.",
	}
)

func init() {
	RootCmd.PersistentFlags().StringVarP(&CfgWorkloadHome, "workloadpath", "w", WorkloadHome, "Workload API home path")
}

// Run the node agent.
func Run() {
	// initialize the workload api.
	wl := wlapi.NewWorkloadAPIServer()

	// create the binder
	b := binder.NewBinder(CfgWorkloadHome)

	pb.RegisterWorkloadServiceServer(b.Server(), wl)

	// Register for system signals
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)

	bstop := make(chan interface{})
	go b.SearchAndBind(bstop)

	<-sigc

	// Shut down the binder
	bstop <- nil
}

func main() {
	if err := RootCmd.Execute(); err != nil {
		log.Fatal(err)
	}

	// Check if the base directory exisits
	_, e := os.Stat(CfgWorkloadHome)
	if e != nil {
		log.Fatalf("workloadApi directory not present (%v)", CfgWorkloadHome)
	}

	Run()
}
