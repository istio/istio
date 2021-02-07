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
	"os/user"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"istio.io/istio/tools/istio-clean-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

var (
	envoyUserVar = env.RegisterStringVar(constants.EnvoyUser, "istio-proxy", "Envoy proxy username")
	// Enable interception of DNS.
	dnsCaptureByAgent = env.RegisterBoolVar("ISTIO_META_DNS_CAPTURE", false,
		"If set to true, enable the capture of outgoing DNS packets on port 53, redirecting to istio-agent on :15053").Get()
)

var rootCmd = &cobra.Command{
	Use:   "istio-clean-iptables",
	Short: "Clean up iptables rules for Istio Sidecar",
	Long:  "Script responsible for cleaning up iptables rules",
	PreRun: func(cmd *cobra.Command, args []string) {
		if err := viper.BindPFlag(constants.DryRun, cmd.Flags().Lookup(constants.DryRun)); err != nil {
			handleError(err)
		}
		viper.SetDefault(constants.DryRun, false)
	},
	Run: func(cmd *cobra.Command, args []string) {
		cfg := constructConfig()
		cleanup(cfg)
	},
}

func bindProxyFlags(cmd *cobra.Command, args []string) {
	if err := viper.BindPFlag(constants.ProxyUID, cmd.Flags().Lookup(constants.ProxyUID)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.ProxyUID, "")

	if err := viper.BindPFlag(constants.ProxyGID, cmd.Flags().Lookup(constants.ProxyGID)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.ProxyGID, "")
}

func constructConfig() *config.Config {
	cfg := &config.Config{
		DryRun:   viper.GetBool(constants.DryRun),
		ProxyUID: viper.GetString(constants.ProxyUID),
		ProxyGID: viper.GetString(constants.ProxyGID),
	}

	// TODO: Make this more configurable, maybe with an allowlist of users to be captured for output instead of a denylist.
	if cfg.ProxyUID == "" {
		usr, err := user.Lookup(envoyUserVar.Get())
		var userID string
		// Default to the UID of ENVOY_USER
		if err != nil {
			userID = constants.DefaultProxyUID
		} else {
			userID = usr.Uid
		}
		cfg.ProxyUID = userID
	}
	// For TPROXY as its uid and gid are same.
	if cfg.ProxyGID == "" {
		cfg.ProxyGID = cfg.ProxyUID
	}

	return cfg
}

func init() {
	// Read in all environment variables
	viper.AutomaticEnv()
	// Replace - with _; so that environment variables are looked up correctly.
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	// https://github.com/spf13/viper/issues/233.
	// The `dry-run` flag is bound in init() across both `istio-iptables` and `istio-clean-iptables` subcommands and
	// will be overwritten by the last. Thus, only adding it here while moving its binding to Viper and value
	// defaulting as part of the command execution.
	rootCmd.Flags().BoolP(constants.DryRun, "n", false, "Do not call any external dependencies like iptables")

	// https://github.com/spf13/viper/issues/233.
	rootCmd.Flags().StringP(constants.ProxyUID, "u", "",
		"Specify the UID of the user for which the redirection is not applied. Typically, this is the UID of the proxy container")

	// https://github.com/spf13/viper/issues/233.
	rootCmd.Flags().StringP(constants.ProxyGID, "g", "",
		"Specify the GID of the user for which the redirection is not applied. (same default value as -u param)")
}

func GetCommand() *cobra.Command {
	return rootCmd
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		handleError(err)
	}
}

func handleError(err error) {
	log.Error(err)
	os.Exit(1)
}
