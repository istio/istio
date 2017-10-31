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

package cmd

import (
	"github.com/spf13/cobra"

	"istio.io/broker/cmd/shared"
	"istio.io/broker/pkg/server"
)

type serverArgs struct {
	port       uint16
	apiPort    uint16
	kubeconfig string
}

func serverCmd(printf, fatalf shared.FormatFn) *cobra.Command {
	sa := &serverArgs{}
	serverCmd := cobra.Command{
		Use:   "server",
		Short: "Starts Broker as a server",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			runServer(sa, printf, fatalf)
		},
	}
	serverCmd.PersistentFlags().Uint16Var(&sa.port, "port", 9091,
		"TCP port to use for Broker's Open Service Broker (OSB) API")
	serverCmd.PersistentFlags().Uint16Var(&sa.apiPort, "apiPort", 9093, "TCP port to use for Broker's gRPC API")
	serverCmd.PersistentFlags().StringVar(&sa.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	return &serverCmd
}

func runServer(sa *serverArgs, printf, fatalf shared.FormatFn) {
	if osb, err := server.CreateServer(sa.kubeconfig); err != nil {
		fatalf("Failed to create server: %s", err.Error())
	} else {
		printf("Server started, listening on port %d", sa.port)
		printf("CTL-C to break out of broker")
		osb.Start(sa.port)
	}
}
