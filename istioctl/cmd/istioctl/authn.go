// Copyright 2018 Istio Authors.
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
	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/writer/pilot"
)

func tlsCheck() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tls-check [<service>]",
		Short: "Check whether TLS setting are matching between authentication policy and destination rules",
		Long: `
Requests Pilot to check for what authentication policy and destination rule it uses for each service in
service registry, and check if TLS settings are compatible between them.
`,
		Example: `
# Check settings for all known services in the service registry:
istioctl authn tls-check

# Check settings for a specific service
istioctl authn tls-check foo.bar.svc.cluster.local
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeClient, err := kubernetes.NewClient(kubeconfig, configContext)
			if err != nil {
				return err
			}
			debug, err := kubeClient.PilotDiscoveryDo(istioNamespace, "GET", "/debug/authenticationz", nil)
			if err != nil {
				return err
			}
			tcw := pilot.TLSCheckWriter{Writer: cmd.OutOrStdout()}
			if len(args) > 0 {
				return tcw.PrintSingle(debug, args[0])
			}
			return tcw.PrintAll(debug)
		},
	}
	return cmd
}

// AuthN provides a command named authn that allows user to interact with Istio authentication policies.
func AuthN() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "authn",
		Short: "Interact with Istio authentication policies",
		Long: `
A group of commands used to interact with Istio authentication policies.
  tls-check
`,
		Example: `# Check whether TLS setting are matching between authentication policy and destination rules:
istioctl authn tls-check`,
	}

	cmd.AddCommand(tlsCheck())
	return cmd
}

func init() {
	rootCmd.AddCommand(AuthN())
}
