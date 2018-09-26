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
	"github.com/spf13/cobra"

	"istio.io/istio/galley/cmd/shared"
	"istio.io/istio/pkg/probe"
)

func probeCmd(printf, fatalf shared.FormatFn) *cobra.Command {
	var (
		probeOptions probe.Options
	)

	probeCmd := &cobra.Command{
		Use:   "probe",
		Short: "Check the liveness or readiness of a locally-running server",
		Run: func(cmd *cobra.Command, _ []string) {
			if err := probeOptions.Validate(); err != nil {
				fatalf("fail on validating probe options: %v", err)
			}
			if err := probe.NewFileClient(&probeOptions).GetStatus(); err != nil {
				fatalf("fail on inspecting path %s: %v", probeOptions.Path, err)
			}
			printf("OK")
		},
	}
	probeCmd.PersistentFlags().StringVar(&probeOptions.Path, "probe-path", "",
		"Path of the file for checking the availability.")
	probeCmd.PersistentFlags().DurationVar(&probeOptions.UpdateInterval, "interval", 0,
		"Duration used for checking the target file's last modified time.")

	return probeCmd
}
