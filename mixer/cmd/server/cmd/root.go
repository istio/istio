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
	"flag"
	"fmt"

	"github.com/spf13/cobra"
	_ "google.golang.org/grpc/grpclog/glogger" // needed to initialize glog

	"istio.io/mixer/cmd/shared"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/template"
)

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string, info map[string]template.Info, adapters []adapter.InfoFn, printf, fatalf shared.FormatFn) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "mixs",
		Short: "Mixer is Istio's abstraction on top of infrastructure backends.",
		Long: "Mixer is Istio's point of integration with infrastructure backends and is the\n" +
			"nexus for policy evaluation and telemetry reporting.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("'%s' is an invalid argument", args[0])
			}
			return nil
		},
	}
	rootCmd.SetArgs(args)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	// hack to make flag.Parsed return true such that glog is happy
	// about the flags having been parsed
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	/* #nosec */
	_ = fs.Parse([]string{})
	flag.CommandLine = fs

	rootCmd.AddCommand(adapterCmd(printf))
	rootCmd.AddCommand(serverCmd(template.NewRepository(info), adapters, printf, fatalf))
	rootCmd.AddCommand(crdCmd(info, adapters, printf))
	rootCmd.AddCommand(shared.VersionCmd(printf))

	return rootCmd
}
