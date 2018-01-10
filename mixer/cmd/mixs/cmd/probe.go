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
	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
)

func probeCmd(printf, fatalf shared.FormatFn) *cobra.Command {
	logOptions := log.NewOptions()
	var path string
	cmd := &cobra.Command{
		Use:   "probe",
		Short: "Check the liveness or readiness of a locally-running server",
		Run: func(cmd *cobra.Command, args []string) {
			if path == "" {
				fatalf("--probe-path is not specified")
			}
			if err := probe.PathExists(path); err != nil {
				fatalf("Fail on inspecting path %s: %v", path, err)
			}
			printf("OK")
		},
	}
	logOptions.AttachCobraFlags(cmd)
	cmd.PersistentFlags().StringVar(&path, "probe-path", "", "Path of the file for checking the availability.")
	return cmd
}
