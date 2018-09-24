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
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	newrelic "istio.io/istio/mixer/adapter/newrelic/pkg"
)

// GetServerCmd returns the root of the cobra command-tree.
func GetServerCmd() *cobra.Command {
	var serverPort string
	var maxWorkers int
	serverCmd := &cobra.Command{
		Use:   "nristioadapter",
		Short: "nristioadapter is a gRPC adapter of Istio Mixer for New Relic backend.",
		Long:  "nristioadapter is a gRPC adapter of Istio Mixer for New Relic backend.\n",
		Run: func(cmd *cobra.Command, args []string) {
			runServer(serverPort, maxWorkers)
		},
	}

	serverCmd.PersistentFlags().StringVarP(&serverPort, "port", "", serverPort,
		"gRPC port for Adapter to communicate with Istio Mixer")
	serverCmd.PersistentFlags().IntVarP(&maxWorkers, "maxworkers", "", maxWorkers,
		"maximal workers to handler requests")
	return serverCmd
}

func runServer(port string, maxWorkers int) {
	newrelic.StartDispatcher(maxWorkers)
	s, err := newrelic.NewGrpcAdapter(port)
	if err != nil {
		fmt.Printf("unable to start sever: %v", err)
		os.Exit(-1)
	}
	shutdown := make(chan error, 1)
	go func() {
		s.Run(shutdown)
	}()
	_ = <-shutdown
}
