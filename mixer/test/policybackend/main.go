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

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"istio.io/istio/pkg/test/fakes/policy"
	"istio.io/pkg/log"
)

var (
	port       int
	address    string
	logOptions *log.Options
)

func main() {

	rootCmd := &cobra.Command{
		Use:          "policybackend",
		Short:        "Fake Policy Backend.",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("'%s' is an invalid argument", args[0])
			}
			return nil
		},
	}

	serviceCmd := &cobra.Command{
		Use:          "server",
		Short:        "Start backend as server.",
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			runServer()
		},
	}
	rootCmd.AddCommand(serviceCmd)

	resetCmd := &cobra.Command{
		Use:          "reset",
		Short:        "Reset a target Policy Backend.",
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			runReset()
		},
	}
	rootCmd.AddCommand(resetCmd)

	rootCmd.SetArgs(os.Args[1:])
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	logOptions = log.DefaultOptions()
	logOptions.AttachCobraFlags(rootCmd)

	rootCmd.PersistentFlags().IntVar(&port, "port", policy.DefaultPort, "GRPC port")
	rootCmd.PersistentFlags().StringVar(&address, "address", "", "Address of the service")

	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("Error during execution: %v", err)
		os.Exit(-1)
	}
}

func runServer() {
	if err := log.Configure(logOptions); err != nil {
		os.Exit(-1)
	}
	log.Infof("Starting up the policy backend: %v", port)

	b := policy.NewPolicyBackend(port)

	if err := b.Start(); err != nil {
		log.Errora(err)
		os.Exit(-1)
	}

	// Wait for the process to be shutdown.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
}

func runReset() {
	if err := log.Configure(logOptions); err != nil {
		os.Exit(-1)
	}

	controller, err := policy.NewController(address)
	if err != nil {
		os.Exit(-1)
	}

	if err := controller.Reset(); err != nil {
		os.Exit(-1)
	}
}
