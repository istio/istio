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
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"istio.io/istio/pilot/pkg/proxy/envoy/configdump"
)

type debug struct {
	JSONPrinter *configdump.JSONPrinter
}

var (
	debugCmd = &cobra.Command{
		Use:   "debug <configuration-type>",
		Short: "Debug local envoy",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			d := &debug{
				JSONPrinter: &configdump.JSONPrinter{
					URL:    "http://127.0.0.1:15000",
					Stdout: os.Stdout,
					Stderr: os.Stderr,
				},
			}
			return d.run(args)
		},
	}
)

func (d *debug) run(args []string) error {
	configType := args[0]
	switch configType {
	case "clusters", "cluster":
		d.JSONPrinter.PrintClusterDump()
	case "listeners", "listener":
		d.JSONPrinter.PrintListenerDump()
	case "bootstrap":
		d.JSONPrinter.PrintBootstrapDump()
	case "routes", "route":
		d.JSONPrinter.PrintRoutesDump()
	case "all":
		d.JSONPrinter.PrintClusterDump()
		d.JSONPrinter.PrintListenerDump()
		d.JSONPrinter.PrintBootstrapDump()
		d.JSONPrinter.PrintRoutesDump()
	default:
		return fmt.Errorf("%q is not a supported debugging config type", configType)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(debugCmd)
}
