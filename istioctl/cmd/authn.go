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

package cmd

import (
	"encoding/json"
	"fmt"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/istioctl/pkg/writer/pilot"
	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
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

# Check settings for pod "foo-656bd7df7c-5zp4s" in namespace default, filtered on destination
service "bar" :
istioctl authn tls-check foo-656bd7df7c-5zp4s.default bar
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("tls-check requires pod name")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeClient, err := clientExecFactory(kubeconfig, configContext)
			if err != nil {
				return err
			}
			podName, ns := handlers.InferPodInfo(args[0], handlers.HandleNamespace(namespace, defaultNamespace))
			results, err := kubeClient.AllPilotsDiscoveryDo(istioNamespace, "GET",
				fmt.Sprintf("/debug/authenticationz?proxyID=%s.%s", podName, ns), nil)
			if err != nil {
				return err
			}

			var debug []v2.AuthenticationDebug
			for i := range results {
				if err := json.Unmarshal(results[i], &debug); err != nil {
					return multierror.Prefix(err, "JSON response invalid:")
				}
				if len(debug) > 0 {
					break
				}
			}
			if len(debug) == 0 {
				return fmt.Errorf("checked %d pilot instances and found no authentication info for %s.%s, check proxy status",
					len(results), podName, ns)
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
