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

package ztunnel

import (
	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/cli"
)

const (
	jsonOutput             = "json"
	yamlOutput             = "yaml"
	summaryOutput          = "short"

	defaultProxyAdminPort = 15000
)

var (
	verboseProxyConfig      bool

	address, node string

	outputFormat string

	proxyAdminPort int

	configDumpFile string

	workloadsNamespace string
)


func Cmd(ctx cli.Context) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "ztunnel-config",
		Short: "Retrieve proxy configuration information from ztunnel [kube only]",
		Long:  `A group of commands used to retrieve information about proxy configuration from the ztunnel`,
		Example: `  # Retrieve information about proxy configuration from a ztunnel instance.
  istioctl ztunnel-config <workload> <pod-name[.namespace]>

  # Retrieve information about proxy configuration from a ztunnel instance with aliases.
  istioctl zc <workload> <pod-name[.namespace]>
`,
		Aliases: []string{"zc"},
	}

	configCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	configCmd.PersistentFlags().IntVar(&proxyAdminPort, "proxy-admin-port", defaultProxyAdminPort, "Ztunnel proxy admin port")

	configCmd.AddCommand(workloadConfigCmd(ctx))

	return configCmd
}
