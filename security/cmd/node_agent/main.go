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
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
	"istio.io/istio/security/cmd/node_agent/na"
	"istio.io/istio/security/pkg/cmd"
)

var (
	naConfig = na.NewConfig()

	rootCmd = &cobra.Command{
		Use:   "node_agent",
		Short: "Istio security per-node agent",

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

	flags := rootCmd.Flags()

	cAClientConfig := &naConfig.CAClientConfig
	flags.StringVar(&cAClientConfig.Org, "org", "", "Organization for the cert")
	flags.DurationVar(&cAClientConfig.RequestedCertTTL, "workload-cert-ttl", 90*24*time.Hour,
		"The requested TTL for the workload")
	flags.IntVar(&cAClientConfig.RSAKeySize, "key-size", 2048, "Size of generated private key")
	flags.StringVar(&cAClientConfig.CAAddress,
		"ca-address", "istio-citadel:8060", "Istio CA address")

	flags.StringVar(&cAClientConfig.Env, "env", "unspecified",
		"Node Environment : unspecified | onprem | gcp | aws | vault")
	flags.StringVar(&cAClientConfig.Platform, "platform", "vm", "The platform istio runs on: vm | k8s")

	flags.StringVar(&cAClientConfig.CertChainFile, "cert-chain",
		"/etc/certs/cert-chain.pem", "Node Agent identity cert file")
	flags.StringVar(&cAClientConfig.KeyFile,
		"key", "/etc/certs/key.pem", "Node Agent private key file")
	flags.StringVar(&cAClientConfig.RootCertFile, "root-cert",
		"/etc/certs/root-cert.pem", "Root Certificate file")
	flags.StringVar(&cAClientConfig.K8sServiceAccountFile, "k8s-service-account",
		"", "K8s service account file")

	naConfig.LoggingOptions.AttachCobraFlags(rootCmd)
	cmd.InitializeFlags(rootCmd)
}

func main() {
	if naConfig.CAClientConfig.Platform == "vm" {
		if err := rootCmd.Execute(); err != nil {
			log.Errora(err)
			os.Exit(-1)
		}
	} else if naConfig.CAClientConfig.Platform == "k8s" {
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
	nodeAgent, err := na.NewNodeAgent(naConfig)
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
