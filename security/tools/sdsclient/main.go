// Copyright Istio Authors. All Rights Reserved.
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

// Program sdsclient simulates a SDS client to test SDS Server, citadel agent.
package main

import (
	"log"

	"github.com/spf13/cobra"

	"istio.io/pkg/version"

	"istio.io/istio/pkg/cmd"
	"istio.io/istio/security/pkg/testing/sdsc"
	"istio.io/pkg/env"
)

var (
	sdsServerUdsPath = env.RegisterStringVar(
		"CITADEL_AGENT_TESTING_UDS_PATH", "unix:///var/run/sds/uds_path",
		"The server unix domain socket path").Get()
	rootCmd = &cobra.Command{
		Use: "sdsclient is used for testing sds server",
		RunE: func(c *cobra.Command, args []string) error {
			client, err := sdsc.NewClient(sdsc.ClientOptions{
				ServerAddress: sdsServerUdsPath,
			})
			if err != nil {
				log.Fatalf("failed to create client, error %v", err)
			}
			if err := client.Send(); err != nil {
				log.Fatalf("failed to send sds request err: %v", err)
			}
			client.Start()
			cmd.WaitSignal(make(chan struct{}))
			return nil
		},
	}
)

func init() {
	rootCmd.AddCommand(version.CobraCommand())
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("failed to start the sdsclient, error %v", err)
	}
}
