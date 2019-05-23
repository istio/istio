// Program sdsclient simulates a SDS client to test SDS Server, citadel agent.
package main

import (
	"log"

	"github.com/spf13/cobra"

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
			client.Send()
			client.Start()
			cmd.WaitSignal(make(chan struct{}))
			return nil
		},
	}
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("failed to start the sdsclient, error %v", err)
	}
}
