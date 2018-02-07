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
	"os"

	"github.com/spf13/cobra"

	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/pkg/version"
	"istio.io/istio/security/pkg/cmd"
)

var (
	logOptions   = log.NewOptions()
	probeOptions = &probe.Options{}

	rootCmd = &cobra.Command{
		Use:   "probe_check",
		Short: "Check the liveness or readiness of a locally-running server",
		Run: func(cmd *cobra.Command, args []string) {
			if !probeOptions.IsValid() {
				shared.Fatalf("probe-path or interval are not valid\n\n%s", cmd.UsageString())
			}
			if err := probe.NewFileClient(probeOptions).GetStatus(); err != nil {
				shared.Fatalf("Fail on inspecting path %s: %v", probeOptions.Path, err)
			}
			shared.Printf("OK")
		},
	}
)

func init() {
	flags := rootCmd.Flags()

	flags.StringVar(&probeOptions.Path, "probe-path", "", "Path of the file for checking the availability.")
	flags.DurationVar(&probeOptions.UpdateInterval, "interval", 0, "Duration used for checking the target file's last modified time.")

	rootCmd.AddCommand(version.CobraCommand())
	logOptions.AttachCobraFlags(rootCmd)

	cmd.InitializeFlags(rootCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Errora(err)
		os.Exit(-1)
	}
}
