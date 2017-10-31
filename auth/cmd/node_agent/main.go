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

package main

import (
	"os"

	"github.com/golang/glog"
	"github.com/spf13/cobra"

	"istio.io/istio/auth/cmd/node_agent/na"
	"istio.io/istio/auth/pkg/cmd"
)

var (
	naConfig na.Config

	rootCmd = &cobra.Command{
		Run: func(cmd *cobra.Command, args []string) {
			runNodeAgent()
		},
	}
)

func init() {
	na.InitializeConfig(&naConfig)

	flags := rootCmd.Flags()

	flags.StringVar(&naConfig.ServiceIdentityOrg, "org", "", "Organization for the cert")
	flags.IntVar(&naConfig.RSAKeySize, "key-size", 1024, "Size of generated private key")
	flags.StringVar(&naConfig.IstioCAAddress,
		"ca-address", "istio-ca:8060", "Istio CA address")
	flags.StringVar(&naConfig.Env, "env", "onprem", "Node Environment : onprem | gcp | aws")
	flags.StringVar(&naConfig.PlatformConfig.CertChainFile, "cert-chain",
		"/etc/certs/cert-chain.pem", "Node Agent identity cert file")
	flags.StringVar(&naConfig.PlatformConfig.KeyFile,
		"key", "/etc/certs/key.pem", "Node identity private key file")
	flags.StringVar(&naConfig.PlatformConfig.RootCACertFile, "root-cert",
		"/etc/certs/root-cert.pem", "Root Certificate file")

	cmd.InitializeFlags(rootCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		glog.Error(err)
		os.Exit(-1)
	}
}

func runNodeAgent() {
	nodeAgent, err := na.NewNodeAgent(&naConfig)
	if err != nil {
		glog.Error(err)
		os.Exit(-1)
	}

	glog.Infof("Starting Node Agent")
	if err := nodeAgent.Start(); err != nil {
		glog.Errorf("Node agent terminated with error: %v.", err)
		os.Exit(-1)
	}
}
