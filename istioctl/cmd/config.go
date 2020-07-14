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

package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	// settableFlags are the flags used to istioctl
	settableFlags = []string{
		"istioNamespace",
		"xds-address",
		"cert-dir",
		"prefer-experimental",
		"xds-port",
		"xds-san",
		"insecure",
	}
)

// configCmd represents the config subcommand command
func configCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config SUBCOMMAND",
		Short: "Gonfigure istioctl defaults",
		Args:  cobra.ExactArgs(0),
		Example: `
# list configuration parameters
istioctl config list
`,
	}
	configCmd.AddCommand(listCommand())
	return configCmd
}

func listCommand() *cobra.Command {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List istio configurable defaults",
		Args:  cobra.ExactArgs(0),
		RunE: func(c *cobra.Command, _ []string) error {
			scope.Debugf("Config file %q", IstioConfig)
			return runList(c.OutOrStdout())
		},
	}
	return listCmd
}

func runList(writer io.Writer) error {
	w := new(tabwriter.Writer).Init(writer, 0, 8, 5, ' ', 0)
	fmt.Fprintf(w, "FLAG\tOVERRIDE\tVALUE\n")
	for _, flag := range settableFlags {
		var override string
		if viper.InConfig(flag) {
			override = viper.GetString(flag)
		}
		fmt.Fprintf(w, "%s\t%s\t%v\n", flag, override, viper.GetString(flag))
	}
	return w.Flush()
}
