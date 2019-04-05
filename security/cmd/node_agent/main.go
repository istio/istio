// Copyright 2019 Istio Authors
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
	"time"

	"istio.io/istio/pkg/env"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
	nvm "istio.io/istio/security/pkg/nodeagent/vm"
)

var (
	naConfig = nvm.NewConfig()

	rootCmd = &cobra.Command{
		Use:   "node_agent",
		Short: "Istio security per-node agent.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			runNodeAgent()
		},
	}
)

func init() {
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Node Agent",
		Section: "node_agent CLI",
		Manual:  "Istio Node Agent",
	}))

	cAClientConfig := &naConfig.CAClientConfig
	cAClientConfig.Org = env.RegisterStringVar(nvm.OrgName, "", "Organization for the cert").Get()
	cAClientConfig.RequestedCertTTL = env.RegisterDurationVar(nvm.RequestedCertTTL, 90*24*time.Hour, "The requested TTL for the workload").Get()
	cAClientConfig.RSAKeySize = env.RegisterIntVar(nvm.RSAKeySize, 2048, "Size of generated private key").Get()
	cAClientConfig.CAAddress = env.RegisterStringVar(nvm.CAAddress, "", "CA's endpoint").Get()
	cAClientConfig.CAProviderName = env.RegisterStringVar(nvm.CAProvider, "", "CA's provider name").Get()
	cAClientConfig.CAProtocol = env.RegisterStringVar(nvm.CAProtocol, nvm.IstioCAService, "CA service protocol").Get()
	cAClientConfig.Env = env.RegisterStringVar(nvm.NodeEnv, "unspecified", "Node Environment : unspecified | onPremVM | gcpVM | awsVM").Get()
	cAClientConfig.Platform = env.RegisterStringVar(nvm.NodePlatform, nvm.VMPlatform, "The platform istio runs on: vm | k8s").Get()
	cAClientConfig.CertChainFile = env.RegisterStringVar(nvm.CertChainFile, "/etc/certs/cert-chain.pem", "Citadel Agent identity cert file").Get()
	cAClientConfig.KeyFile = env.RegisterStringVar(nvm.KeyFile, "/etc/certs/key.pem", "Citadel Agent private key file").Get()
	cAClientConfig.RootCertFile = env.RegisterStringVar(nvm.RootCertFile, "/etc/certs/root-cert.pem", "Citadel Agent root cert file").Get()

	naConfig.DualUse = env.RegisterBoolVar(nvm.DualUse, false, "Enable dual-use mode. Generates certificates with a CommonName identical to the SAN.").Get()
}

func main() {
	if naConfig.CAClientConfig.Platform == nvm.VMPlatform {
		if err := rootCmd.Execute(); err != nil {
			log.Errora(err)
			os.Exit(-1)
		}
	} else if naConfig.CAClientConfig.Platform == nvm.K8sPlatform {
		log.Errorf("WIP for support on k8s...")
		os.Exit(-1)
	} else {
		log.Errorf("node agent on %v is not supported yet", naConfig.CAClientConfig.Platform)
		os.Exit(-1)
	}
}

func runNodeAgent() {
	if err := log.Configure(naConfig.LoggingOptions); err != nil {
		log.Errora(err)
		os.Exit(-1)
	}
	nodeAgent, err := nvm.NewNodeAgent(naConfig)
	if err != nil {
		log.Errora(err)
		os.Exit(-1)
	}

	log.Infof("Starting Node Agent")
	if err := nodeAgent.Start(); err != nil {
		log.Errorf("Node agent terminated with error: %v.", err)
		os.Exit(-1)
	}
}
