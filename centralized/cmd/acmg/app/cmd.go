// Copyright Istio Authors
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

package app

import (
	"fmt"
	"github.com/spf13/cobra"
	"istio.io/istio/centralized/pkg/server"
	"istio.io/istio/pkg/cmd"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

var (
	logOptions = log.DefaultOptions()
)

var rootCmd = &cobra.Command{
	Use:          "acmg-controller",
	Short:        "Based on CoreDns. hijack service traffic to centralized gateway.",
	SilenceUsage: true,
	PreRunE: func(c *cobra.Command, args []string) error {
		if err := log.Configure(logOptions); err != nil {
			log.Errorf("Failed to configure log %v", err)
		}
		return nil
	},
	RunE: func(c *cobra.Command, args []string) (err error) {
		cmd.PrintFlags(c.Flags())
		ctx := c.Context()
		server, err := server.NewServer(ctx, server.CoreDnsHijackArgs{
			GatewayNamespace:       server.GatewayNamespace,
			GatewayServiceName:     server.GatewayServiceName,
			GateWayName:            server.IstioGatewayName,
			CentralizedGateWayName: server.CentralizedGateWayAppName,
		})
		if err != nil {
			return fmt.Errorf("failed to create acmg informer service: %v", err)
		}
		if err = server.PreStartCheck(); err != nil {
			log.Errorf(err)
			return
		}

		if err = server.Init(); err != nil {
			log.Errorf(err)
			return
		}

		server.Start()

		<-ctx.Done()
		return
	},
}

// GetCommand returns the main cobra.Command object for this application
func GetCommand() *cobra.Command {
	return rootCmd
}

func init() {
	rootCmd.AddCommand(version.CobraCommand())
	logOptions.AttachCobraFlags(rootCmd)
}
