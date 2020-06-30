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
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"istio.io/istio/cni/pkg/install-cni/pkg/constants"
	"istio.io/istio/cni/pkg/install-cni/pkg/install"
	"istio.io/pkg/log"
)

var rootCmd = &cobra.Command{
	Use:   "install-cni",
	Short: "Install and configure CNI on a node",
	RunE: func(cmd *cobra.Command, args []string) error {
		return install.Run()
	},
}

// GetCommand returns the main cobra.Command object for this application
func GetCommand() *cobra.Command {
	return rootCmd
}

func init() {
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	registerStringParameter(constants.CNINetDir, "/etc/cni/net.d", "Directory on the host where CNI networks are installed")
	registerStringParameter(constants.MountedCNINetDir, "/host/etc/cni/net.d", "Directory on the container where CNI networks are installed")
	registerStringParameter(constants.CNIConfName, "", "Name of the CNI configuration file")
	registerStringParameter(constants.KubeCfgFilename, "ZZZ-istio-cni-kubeconfig", "Name of the kubeconfig file")
	registerStringParameter(constants.CNINetworkConfig, "", "CNI config template as a string")
	registerStringParameter(constants.CNINetworkConfigFile, "", "CNI config template as a file")
	registerStringParameter(constants.KubeCAFile, "", "CA file for kubeconfig. Defaults to the pod one")
	registerStringParameter(constants.LogLevel, "warn", "Fallback value for log level in CNI config file, if not specified in helm template")

	registerStringArrayParameter(constants.SkipCNIBinaries, []string{}, "Binaries that should not be installed")

	registerBooleanParameter(constants.ChainedCNIPlugin, true, "Whether to install CNI plugin as a chained or standalone")
	registerBooleanParameter(constants.UpdateCNIBinaries, true, "Update binaries")
	registerBooleanParameter(constants.SkipTLSVerify, false, "Whether to use insecure TLS in kubeconfig file")
}

func registerStringParameter(name, value, usage string) {
	rootCmd.Flags().String(name, value, usage)
	bindViper(name)
}

func registerStringArrayParameter(name string, value []string, usage string) {
	rootCmd.Flags().StringArray(name, value, usage)
	bindViper(name)
}

func registerBooleanParameter(name string, value bool, usage string) {
	rootCmd.Flags().Bool(name, value, usage)
	bindViper(name)
}

func bindViper(name string) {
	if err := viper.BindPFlag(name, rootCmd.Flags().Lookup(name)); err != nil {
		log.Errora(err)
		os.Exit(1)
	}
}
