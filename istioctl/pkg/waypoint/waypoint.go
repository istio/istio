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
	"cmp"
	"context"
	"fmt"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/yaml"

	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/completion"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

var (
	revision      = ""
	waitReady     bool
	allNamespaces bool

	deleteAll bool

	trafficType string
)

const (
	waitTimeout = 90 * time.Second
)

var validTrafficTypes = sets.New(constants.ServiceTraffic, constants.WorkloadTraffic, constants.AllTraffic, constants.NoTraffic)

func Cmd(ctx cli.Context) *cobra.Command {
	var waypointServiceAccount string
	makeGatewayName := func(sa string) string {
		name := sa
		if name == "" {
			name = "namespace"
		}
		return name
	}
	makeGateway := func(forApply bool) (*gateway.Gateway, error) {
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

		// Determine which traffic address type to apply the waypoint to, if none
		// then default to "service" as the waypoint-for traffic type.
		if trafficType == "" {
			trafficType = constants.ServiceTraffic
		}
		if !validTrafficTypes.Contains(trafficType) {
			return nil, fmt.Errorf("invalid traffic type: %s. Valid options are: %s", trafficType, validTrafficTypes.String())
		}

		if gw.Annotations == nil {
			gw.Annotations = map[string]string{}
		}
		gw.Annotations[constants.WaypointForAddressType] = trafficType

		if waypointServiceAccount != "" {
			gw.Annotations = map[string]string{
				constants.WaypointServiceAccount: waypointServiceAccount,
			}
		}
		if revision != "" {
			gw.Labels = map[string]string{label.IoIstioRev.Name: revision}
		}
		return &gw, nil
	}
	waypointGenerateCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a waypoint configuration",
		Long:  "Generate a waypoint configuration as YAML",
		Example: `  # Generate a waypoint as yaml
  istioctl x waypoint generate --service-account something --namespace default`,
		RunE: func(cmd *cobra.Command, args []string) error {
			gw, err := makeGateway(false)
			if err != nil {
				return err
			}
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

  # Apply a waypoint to a specific namespace for a specific service account and wait for it to be ready
  istioctl x waypoint apply --service-account something --namespace default --wait`,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClientWithRevision(revision)
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}
			gw, err := makeGateway(true)
			if err != nil {
				return err
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
			if waitReady {
				startTime := time.Now()
				ticker := time.NewTicker(1 * time.Second)
				defer ticker.Stop()
				for range ticker.C {
					programmed := false
					gwc, err := kubeClient.GatewayAPI().GatewayV1beta1().Gateways(ctx.NamespaceOrDefault(ctx.Namespace())).Get(context.TODO(), gw.Name, metav1.GetOptions{})
					if err == nil {
						// Check if gateway has Programmed condition set to true
						for _, cond := range gwc.Status.Conditions {
							if cond.Type == string(gatewayv1.GatewayConditionProgrammed) && string(cond.Status) == "True" {
								programmed = true
								break
							}
						}
					}
					if programmed {
						break
					}
					if time.Since(startTime) > waitTimeout {
						errorMsg := fmt.Sprintf("timed out while waiting for waypoint %v/%v", gw.Namespace, gw.Name)
						if err != nil {
							errorMsg += fmt.Sprintf(": %s", err)
						}
						return fmt.Errorf(errorMsg)
					}
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "waypoint %v/%v applied\n", gw.Namespace, gw.Name)
			return nil
		},
	}
	waypointApplyCmd.PersistentFlags().StringVar(&trafficType,
		"for",
		"service",
		fmt.Sprintf("Specify the traffic type %s for the waypoint", validTrafficTypes.String()),
	)

	waypointDeleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a waypoint configuration",
		Long:  "Delete a waypoint configuration from the cluster",
		Example: `  # Delete a waypoint from the default namespace
  istioctl x waypoint delete

  # Delete a waypoint from a specific namespace for a specific service account
  istioctl x waypoint delete --service-account something --namespace default

  # Delete a waypoint by name, which can obtain from istioctl x waypoint list
  istioctl x waypoint delete waypoint-name --namespace default

  # Delete several waypoints by name
  istioctl x waypoint delete waypoint-name1 waypoint-name2 --namespace default

  # Delete all waypoints in a specific namespace
  istioctl x waypoint delete --all --namespace default`,
		Args: func(cmd *cobra.Command, args []string) error {
			if deleteAll && len(args) > 0 {
				return fmt.Errorf("cannot specify waypoint names when deleting all waypoints")
			}
			if deleteAll && waypointServiceAccount != "" {
				return fmt.Errorf("cannot specify service account when deleting all waypoints")
			}
			if len(args) > 0 && waypointServiceAccount != "" {
				return fmt.Errorf("cannot specify service account when deleting by name")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}
			ns := ctx.NamespaceOrDefault(ctx.Namespace())

			// Delete all waypoints if the --all flag is set
			if deleteAll {
				return deleteWaypoints(cmd, kubeClient, ns, nil)
			}

			// Delete waypoints by service account if provided
			if len(args) == 0 {
				gw, err := makeGateway(true)
				if err != nil {
					return err
				}
				if err = kubeClient.GatewayAPI().GatewayV1beta1().Gateways(gw.Namespace).
					Delete(context.Background(), gw.Name, metav1.DeleteOptions{}); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "waypoint %v/%v deleted\n", gw.Namespace, gw.Name)
				return nil
			}

			// Delete waypoints by names if provided
			return deleteWaypoints(cmd, kubeClient, ns, args)
		},
	}
	waypointDeleteCmd.Flags().BoolVar(&deleteAll, "all", false, "Delete all waypoints in the namespace")

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
			slices.SortFunc(gws.Items, func(i, j gateway.Gateway) int {
				if r := cmp.Compare(i.Namespace, j.Namespace); r != 0 {
					return r
				}
				return cmp.Compare(i.Name, j.Name)
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
					if cond.Type == string(gatewayv1.GatewayConditionProgrammed) {
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
	waypointApplyCmd.PersistentFlags().BoolVarP(&waitReady, "wait", "w", false, "Wait for the waypoint to be ready")
	waypointCmd.AddCommand(waypointApplyCmd)
	waypointGenerateCmd.PersistentFlags().StringVarP(&revision, "revision", "r", "", "The revision to label the waypoint with")
	waypointCmd.AddCommand(waypointGenerateCmd)
	waypointCmd.AddCommand(waypointDeleteCmd)
	waypointCmd.AddCommand(waypointListCmd)
	waypointCmd.PersistentFlags().StringVarP(&waypointServiceAccount, "service-account", "s", "", "service account to create a waypoint for")

	_ = waypointCmd.RegisterFlagCompletionFunc("service-account", func(
		cmd *cobra.Command, args []string, toComplete string,
	) ([]string, cobra.ShellCompDirective) {
		return completion.ValidServiceAccountArgs(cmd, ctx, args, toComplete)
	})

	return waypointCmd
}

// deleteWaypoints handles the deletion of waypoints based on the provided names, or all if names is nil
func deleteWaypoints(cmd *cobra.Command, kubeClient kube.CLIClient, namespace string, names []string) error {
	var multiErr *multierror.Error
	if names == nil {
		// If names is nil, delete all waypoints
		waypoints, err := kubeClient.GatewayAPI().GatewayV1beta1().Gateways(namespace).
			List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, gw := range waypoints.Items {
			names = append(names, gw.Name)
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if err := kubeClient.GatewayAPI().GatewayV1beta1().Gateways(namespace).
				Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil {
				if errors.IsNotFound(err) {
					fmt.Fprintf(cmd.OutOrStdout(), "waypoint %v/%v not found\n", namespace, name)
				} else {
					mu.Lock()
					multiErr = multierror.Append(multiErr, err)
					mu.Unlock()
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "waypoint %v/%v deleted\n", namespace, name)
			}
		}(name)
	}

	wg.Wait()
	return multiErr.ErrorOrNil()
}
