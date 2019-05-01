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
	"fmt"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/writer/pilot"
)

func tlsCheck() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tls-check <pod-name[.namespace]> [<service>]",
		Short: "Check whether TLS setting are matching between authentication policy and destination rules",
		Long: `
Check what authentication policies and destination rules pilot uses to config a proxy instance,
and check if TLS settings are compatible between them.
`,
		Example: `
# Check settings for pod "foo-656bd7df7c-5zp4s" in namespace default:
istioctl authn tls-check foo-656bd7df7c-5zp4s.default

# Check settings for pod "foo-656bd7df7c-5zp4s" in namespace default, filtered on destintation
service "bar" :
istioctl authn tls-check foo-656bd7df7c-5zp4s.default bar
`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeClient, err := clientExecFactory(kubeconfig, configContext)
			if err != nil {
				return err
			}
			podName, ns := inferPodInfo(args[0], handleNamespace())
			debug, err := kubeClient.PilotDiscoveryDo(istioNamespace, "GET",
				fmt.Sprintf("/debug/authenticationz?proxyID=%s.%s", podName, ns), nil)
			if err != nil {
				return err
			}
			tcw := pilot.TLSCheckWriter{Writer: cmd.OutOrStdout()}
			if len(args) >= 2 {
				return tcw.PrintSingle(debug, args[1])
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
