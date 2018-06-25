// Copyright 2018 Istio Authors
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
	"strings"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/pkg/version"
)

type serverContainerCmd struct {
	// Container to apply the command upon
	container string

	// Command to get full version details
	cmd string

	// Command to get short version info
	cmdShort string
}

var (
	// Create a kubernetes.Client (or mock)
	kClientFactory = kubernetes.NewClient

	// Version format
	shortVersion bool

	// Version include servers
	remoteVersion bool

	istioPodCmds = map[string][]serverContainerCmd{
		"istio=pilot": {{"discovery",
			"/usr/local/bin/pilot-discovery version",
			"/usr/local/bin/pilot-discovery version -s"}},
		"istio=citadel": {{"citadel",
			"/usr/local/bin/istio_ca version",
			"/usr/local/bin/istio_ca version -s"}},
		"istio=egressgateway": {{"egressgateway",
			"/usr/local/bin/pilot-agent version",
			"/usr/local/bin/pilot-agent version -s"}},
		"istio=galley": {{"validator",
			"/usr/local/bin/gals version",
			"/usr/local/bin/gals version -s"}},
		"istio=ingressgateway": {{"ingressgateway",
			"/usr/local/bin/pilot-agent version",
			"/usr/local/bin/pilot-agent version -s"}},
		"istio-mixer-type=policy": {{"mixer",
			"/usr/local/bin/mixs version",
			"/usr/local/bin/mixs version -s"}},
		"istio-mixer-type=telemetry": {{"mixer",
			"/usr/local/bin/mixs version",
			"/usr/local/bin/mixs version -s"}},
		"istio=sidecar-injector": {{"sidecar-injector-webhook",
			"/usr/local/bin/sidecar-injector version",
			"/usr/local/bin/sidecar-injector version -s"}},
	}
	// TODO Should I add "/usr/local/bin/envoy --version" invocations?

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Prints out build version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			if shortVersion {
				cmd.Println(version.Info)
			} else {
				cmd.Println(version.Info.LongForm())
			}
			if !remoteVersion {
				return nil
			}

			kubeClient, err := kClientFactory(kubeconfig, configContext)
			if err != nil {
				return err
			}
			var errs error
			for selector, remoteCmds := range istioPodCmds {
				for _, remoteCmd := range remoteCmds {
					var vcmd string
					if shortVersion {
						vcmd = remoteCmd.cmdShort
					} else {
						vcmd = remoteCmd.cmd
					}
					results, err := kubeClient.AllSelectorDiscoveryExec(istioNamespace, selector, remoteCmd.container, strings.Split(vcmd, " "))
					if err != nil {
						errs = multierror.Append(errs, err)
						continue
					}
					for server, result := range results {
						if shortVersion {
							cmd.Printf("%s: %s", server, string(result))
						} else {
							cmd.Printf("%s:\n%s", server, string(result))
						}
					}
				}
			}
			return errs
		},
	}
)

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.PersistentFlags().BoolVarP(&shortVersion, "short", "s", shortVersion, "Displays a short form of the version information")
	versionCmd.PersistentFlags().BoolVarP(&remoteVersion, "remote", "r", remoteVersion, "Displays version information about server components")
}
