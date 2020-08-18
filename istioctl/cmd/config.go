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
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/pkg/env"
)

var (
	// settableFlags are the flags used to istioctl
	settableFlags = map[string]interface{}{
		"istioNamespace":      env.RegisterStringVar("ISTIOCTL_ISTIONAMESPACE", controller.IstioNamespace, "istioctl --istioNamespace override"),
		"xds-address":         env.RegisterStringVar("ISTIOCTL_XDS_ADDRESS", "", "istioctl --xds-address override"),
		"xds-port":            env.RegisterIntVar("ISTIOCTL_XDS_PORT", 15012, "istioctl --xds-port override"),
		"authority":           env.RegisterStringVar("ISTIOCTL_AUTHORITY", "", "istioctl --authority override"),
		"cert-dir":            env.RegisterStringVar("ISTIOCTL_CERT_DIR", "", "istioctl --cert-dir override"),
		"insecure":            env.RegisterBoolVar("ISTIOCTL_INSECURE", false, "istioctl --insecure override"),
		"prefer-experimental": env.RegisterBoolVar("ISTIOCTL_PREFER_EXPERIMENTAL", false, "istioctl should use experimental subcommand variants"),
	}
)

// configCmd represents the config subcommand command
func configCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config SUBCOMMAND",
		Short: "Configure istioctl defaults",
		Args:  cobra.NoArgs,
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

func configSource(flag string, v interface{}) string {
	// Environment variables have high precedence in Viper
	if isVarSet(v) {
		return "$" + getVarVar(v).Name
	}

	if viper.InConfig(flag) {
		return IstioConfig
	}

	return "default"
}

func getVarVar(v interface{}) env.Var {
	switch ev := v.(type) {
	case env.StringVar:
		return ev.Var
	case env.BoolVar:
		return ev.Var
	case env.IntVar:
		return ev.Var
	case env.DurationVar:
		return ev.Var
	case env.FloatVar:
		return ev.Var
	default:
		panic(fmt.Sprintf("Unexpected environment var type %v", v))
	}
}

func isVarSet(v interface{}) bool {
	switch ev := v.(type) {
	case env.StringVar:
		_, ok := ev.Lookup()
		return ok
	case env.BoolVar:
		_, ok := ev.Lookup()
		return ok
	case env.IntVar:
		_, ok := ev.Lookup()
		return ok
	case env.DurationVar:
		_, ok := ev.Lookup()
		return ok
	case env.FloatVar:
		_, ok := ev.Lookup()
		return ok
	default:
		panic(fmt.Sprintf("Unexpected environment var type %v", v))
	}
}
