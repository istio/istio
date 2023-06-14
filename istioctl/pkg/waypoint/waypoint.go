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

package waypoint

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/yaml"

	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/slices"
)

var (
	revision = ""

	allNamespaces bool
)

func Cmd(ctx cli.Context) *cobra.Command {
	var waypointServiceAccount string
	makeGatewayName := func(sa string) string {
		name := sa
		if name == "" {
			name = "namespace"
		}
		return name
	}
	makeGateway := func(forApply bool) *gateway.Gateway {
		ns := ctx.NamespaceOrDefault(ctx.Namespace())
		if ctx.Namespace() == "" && !forApply {
			ns = ""
		}
		gw := gateway.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       gvk.KubernetesGateway.Kind,
				APIVersion: gvk.KubernetesGateway.GroupVersion(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      makeGatewayName(waypointServiceAccount),
				Namespace: ns,
			},
			Spec: gateway.GatewaySpec{
				GatewayClassName: constants.WaypointGatewayClassName,
				Listeners: []gateway.Listener{{
					Name:     "mesh",
					Port:     15008,
					Protocol: gateway.ProtocolType(protocol.HBONE),
				}},
			},
		}
		if waypointServiceAccount != "" {
			gw.Annotations = map[string]string{
				constants.WaypointServiceAccount: waypointServiceAccount,
			}
		}
		if revision != "" {
			gw.Labels = map[string]string{label.IoIstioRev.Name: revision}
		}
		return &gw
	}
	waypointGenerateCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a waypoint configuration",
		Long:  "Generate a waypoint configuration as YAML",
		Example: `  # Generate a waypoint as yaml
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
  istioctl x waypoint apply

  # Apply a waypoint to a specific namespace for a specific service account
  istioctl x waypoint apply --service-account something --namespace default`,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}
			gw := makeGateway(true)
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}
			gwc := kubeClient.GatewayAPI().GatewayV1beta1().Gateways(ctx.NamespaceOrDefault(ctx.Namespace()))
			b, err := yaml.Marshal(gw)
			if err != nil {
				return err
			}
			_, err = gwc.Patch(context.Background(), gw.Name, types.ApplyPatchType, b, metav1.PatchOptions{
				Force:        nil,
				FieldManager: "istioctl",
			})
			if err != nil {
				if errors.IsNotFound(err) {
					return fmt.Errorf("missing Kubernetes Gateway CRDs need to be installed before applying a waypoint: %s", err)
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "waypoint %v/%v applied\n", gw.Namespace, gw.Name)
			return nil
		},
	}
	waypointDeleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a waypoint configuration",
		Long:  "Delete a waypoint configuration from the cluster",
		Example: `  # Delete a waypoint from the default namespace
  istioctl x waypoint delete
  
  # Delete a waypoint from a specific namespace for a specific service account
  istioctl x waypoint delete --service-account something --namespace default

  # Delete a waypoint by name, which can obtain from istioctl x waypoint list 
  istioctl x waypoint delete waypoint-name --namespace default`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("too many arguments, expected 0 or 1")
			}
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}
			if len(args) == 1 {
				name := args[0]
				ns := ctx.NamespaceOrDefault(ctx.Namespace())
				gw, err := kubeClient.GatewayAPI().GatewayV1beta1().Gateways(ns).Get(context.Background(), name, metav1.GetOptions{})
				if err != nil {
					if errors.IsNotFound(err) {
						fmt.Fprintf(cmd.OutOrStdout(), "waypoint %v/%v not found\n", ns, name)
						return nil
					}
					return err
				}
				if err := kubeClient.GatewayAPI().GatewayV1beta1().Gateways(ns).
					Delete(context.Background(), gw.Name, metav1.DeleteOptions{}); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "waypoint %v/%v deleted\n", ns, name)
				return nil
			}
			gw := makeGateway(true)
			if err = kubeClient.GatewayAPI().GatewayV1beta1().Gateways(gw.Namespace).
				Delete(context.Background(), gw.Name, metav1.DeleteOptions{}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "waypoint %v/%v deleted\n", gw.Namespace, gw.Name)
			return nil
		},
	}
	waypointListCmd := &cobra.Command{
		Use:   "list",
		Short: "List managed waypoint configurations",
		Long:  "List managed waypoint configurations in the cluster",
		Example: `  # List all waypoints in a specific namespace
  istioctl x waypoint list --namespace default

  # List all waypoints in the cluster
  istioctl x waypoint list -A`,
		RunE: func(cmd *cobra.Command, args []string) error {
			writer := cmd.OutOrStdout()
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}
			var ns string
			if allNamespaces {
				ns = ""
			} else {
				ns = ctx.NamespaceOrDefault(ctx.Namespace())
			}
			gws, err := kubeClient.GatewayAPI().GatewayV1beta1().Gateways(ns).
				List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			if len(gws.Items) == 0 {
				fmt.Fprintln(writer, "No waypoints found.")
				return nil
			}
			w := new(tabwriter.Writer).Init(writer, 0, 8, 5, ' ', 0)
			slices.SortFunc(gws.Items, func(i, j gateway.Gateway) bool {
				if i.Namespace <= j.Namespace {
					return true
				}
				if i.Namespace > j.Namespace {
					return false
				}
				return i.Name < j.Name
			})
			filteredGws := make([]gateway.Gateway, 0)
			for _, gw := range gws.Items {
				if gw.Spec.GatewayClassName != constants.WaypointGatewayClassName {
					continue
				}
				filteredGws = append(filteredGws, gw)
			}
			if allNamespaces {
				fmt.Fprintln(w, "NAMESPACE\tNAME\tSERVICE ACCOUNT\tREVISION\tPROGRAMMED")
			} else {
				fmt.Fprintln(w, "NAME\tSERVICE ACCOUNT\tREVISION\tPROGRAMMED")
			}
			for _, gw := range filteredGws {
				sa := gw.Annotations[constants.WaypointServiceAccount]
				programmed := kstatus.StatusFalse
				rev := gw.Labels[label.IoIstioRev.Name]
				if rev == "" {
					rev = "default"
				}
				for _, cond := range gw.Status.Conditions {
					if cond.Type == string(gateway.GatewayConditionProgrammed) {
						programmed = string(cond.Status)
					}
				}
				if allNamespaces {
					_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", gw.Namespace, gw.Name, sa, rev, programmed)
				} else {
					_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", gw.Name, sa, rev, programmed)
				}
			}
			return w.Flush()
		},
	}
	waypointListCmd.PersistentFlags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "List all waypoints in all namespaces")

	waypointCmd := &cobra.Command{
		Use:   "waypoint",
		Short: "Manage waypoint configuration",
		Long:  "A group of commands used to manage waypoint configuration",
		Example: `  # Apply a waypoint to the current namespace
  istioctl x waypoint apply

  # Generate a waypoint as yaml
  istioctl x waypoint generate --service-account something --namespace default

  # Delete a waypoint from a specific namespace for a specific service account
  istioctl x waypoint delete --service-account something --namespace default

  # List all waypoints in a specific namespace
  istioctl x waypoint list --namespace default`,
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

	waypointApplyCmd.PersistentFlags().StringVarP(&revision, "revision", "r", "", "The revision to label the waypoint with")
	waypointCmd.AddCommand(waypointApplyCmd)
	waypointGenerateCmd.PersistentFlags().StringVarP(&revision, "revision", "r", "", "The revision to label the waypoint with")
	waypointCmd.AddCommand(waypointGenerateCmd)
	waypointCmd.AddCommand(waypointDeleteCmd)
	waypointCmd.AddCommand(waypointListCmd)
	waypointCmd.PersistentFlags().StringVarP(&waypointServiceAccount, "service-account", "s", "", "service account to create a waypoint for")

	return waypointCmd
}
