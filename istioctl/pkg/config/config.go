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

package config

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"istio.io/istio/istioctl/pkg/root"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/env"
)

// settableFlags are the flags used to istioctl
var settableFlags = map[string]env.VariableInfo{
	"istioNamespace":      env.Register("ISTIOCTL_ISTIONAMESPACE", constants.IstioSystemNamespace, "The istioctl --istioNamespace override"),
	"xds-address":         env.Register("ISTIOCTL_XDS_ADDRESS", "", "The istioctl --xds-address override"),
	"xds-port":            env.Register("ISTIOCTL_XDS_PORT", 15012, "The istioctl --xds-port override"),
	"authority":           env.Register("ISTIOCTL_AUTHORITY", "", "The istioctl --authority override"),
	"cert-dir":            env.Register("ISTIOCTL_CERT_DIR", "", "The istioctl --cert-dir override"),
	"insecure":            env.Register("ISTIOCTL_INSECURE", false, "The istioctl --insecure override"),
	"prefer-experimental": env.Register("ISTIOCTL_PREFER_EXPERIMENTAL", false, "The istioctl should use experimental subcommand variants"),
	"plaintext":           env.Register("ISTIOCTL_PLAINTEXT", false, "The istioctl --plaintext override"),
}

// Cmd represents the config subcommand command
func Cmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config SUBCOMMAND",
		Short: "Configure istioctl defaults",
		Args:  cobra.NoArgs,
		Example: `  # list configuration parameters
  istioctl config list`,
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
			root.Scope.Debugf("Config file %q", root.IstioConfig)
			return runList(c.OutOrStdout())
		},
	}
	return listCmd
}

func runList(writer io.Writer) error {
	// Sort flag names
	keys := make([]string, len(settableFlags))
	i := 0
	for key := range settableFlags {
		keys[i] = key
		i++
	}
	sort.Strings(keys)
	w := new(tabwriter.Writer).Init(writer, 0, 8, 5, ' ', 0)
	fmt.Fprintf(w, "FLAG\tVALUE\tFROM\n")
	for _, flag := range keys {
		v := settableFlags[flag]
		fmt.Fprintf(w, "%s\t%s\t%v\n", flag, viper.GetString(flag), configSource(flag, v))
	}
	return w.Flush()
}

func configSource(flag string, v env.VariableInfo) string {
	// Environment variables have high precedence in Viper
	if v.IsSet() {
		return "$" + v.GetName()
	}

	if viper.InConfig(flag) {
		return root.IstioConfig
	}

	return "default"
}
