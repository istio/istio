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
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/yaml"

	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
)

func waypointCmd() *cobra.Command {
	var waypointServiceAccount string
	makeGateway := func(forApply bool) *gateway.Gateway {
		ns := handlers.HandleNamespace(namespace, defaultNamespace)
		if namespace == "" && !forApply {
			ns = ""
		}
		name := waypointServiceAccount
		if name == "" {
			name = "namespace"
		}
		gw := gateway.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       gvk.KubernetesGateway.Kind,
				APIVersion: gvk.KubernetesGateway.GroupVersion(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
			Spec: gateway.GatewaySpec{
				GatewayClassName: constants.WaypointGatewayClassName,
				Listeners: []gateway.Listener{{
					Name:     "mesh",
					Port:     15008,
					Protocol: "ALL",
				}},
			},
		}
		if waypointServiceAccount != "" {
			gw.Annotations = map[string]string{
				constants.WaypointServiceAccount: waypointServiceAccount,
			}
		}
		return &gw
	}
	waypointGenerateCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a waypoint configuration",
		Long:  "Generate a waypoint configuration as YAML",
		Example: ` # Generate a waypoint as yaml
  istioctl x waypoint generate --service-account something --namespace default`,
		RunE: func(cmd *cobra.Command, args []string) error {
			gw := makeGateway(false)
			b, err := yaml.Marshal(gw)
			if err != nil {
				return err
			}
			// strip junk
			res := strings.ReplaceAll(string(b), `  creationTimestamp: null
`, "")
			res = strings.ReplaceAll(res, `status: {}
`, "")
			fmt.Fprint(cmd.OutOrStdout(), res)
			return nil
		},
	}
	waypointApplyCmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply a waypoint configuration",
		Long:  "Apply a waypoint configuration to the cluster",
		Example: `  # Apply a waypoint to the current namespace
  istioctl x waypoint apply`,
		RunE: func(cmd *cobra.Command, args []string) error {
			gw := makeGateway(true)
			client, err := kubeClient(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}
			gwc := client.GatewayAPI().GatewayV1beta1().Gateways(handlers.HandleNamespace(namespace, defaultNamespace))
			b, err := yaml.Marshal(gw)
			if err != nil {
				return err
			}
			_, err = gwc.Patch(context.Background(), gw.Name, types.ApplyPatchType, b, metav1.PatchOptions{
				Force:        nil,
				FieldManager: "istioctl",
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "waypoint %v/%v applied\n", gw.Namespace, gw.Name)
			return nil
		},
	}
	waypointCmd := &cobra.Command{
		Use:   "waypoint",
		Short: "Manage waypoint configuration",
		Long:  "A group of commands used to manage waypoint configuration",
		Example: `  # Apply a waypoint to the current namespace
  istioctl x waypoint apply

  # Generate a waypoint as yaml
  istioctl x waypoint generate --service-account something --namespace default`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("unknown subcommand %q", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			return nil
		},
	}

	waypointCmd.AddCommand(waypointApplyCmd)
	waypointCmd.AddCommand(waypointGenerateCmd)
	waypointCmd.PersistentFlags().StringVarP(&waypointServiceAccount, "service-account", "s", "", "service account to create a waypoint for")

	return waypointCmd
}
