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

package ipaddress

import (
	"fmt"
	"net/netip"

	"github.com/spf13/cobra"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/cli"
)

const (
	originalServiceNameLabel      = "istio.io/original-service-name"
	originalServiceNamespaceLabel = "istio.io/original-service-namespace"
)

type createOptions struct {
	serviceName       string
	originNS          string
	localServiceName  string
	localServiceEntry string
	localNS           string
}

func Cmd(ctx cli.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sentinel-ipaddress",
		Short: "Manage Kubernetes IPAddress resources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(createCmd(ctx))
	return cmd
}

func createCmd(ctx cli.Context) *cobra.Command {
	opts := createOptions{}
	cmd := &cobra.Command{
		Use:   "create IP",
		Short: "Create a Kubernetes IPAddress resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (opts.localServiceName == "") == (opts.localServiceEntry == "") {
				return fmt.Errorf("exactly one of --local-service-name or --local-service-entry must be set")
			}

			parsedAddress, err := netip.ParseAddr(args[0])
			if err != nil {
				return fmt.Errorf("invalid IP address %q: %v", args[0], err)
			}
			address := parsedAddress.String()

			namespace := opts.localNS
			if namespace == "" {
				namespace = ctx.NamespaceOrDefault(ctx.Namespace())
			}
			client, err := ctx.CLIClient()
			if err != nil {
				return fmt.Errorf("creating Kubernetes client: %v", err)
			}

			labels := make(map[string]string, 2)
			if opts.serviceName != "" {
				labels[originalServiceNameLabel] = opts.serviceName
			}
			if opts.originNS != "" {
				labels[originalServiceNamespaceLabel] = opts.originNS
			}

			parentRef := &networkingv1.ParentReference{
				Resource:  "services",
				Namespace: namespace,
				Name:      opts.localServiceName,
			}
			if opts.localServiceEntry != "" {
				parentRef.Group = "networking.istio.io"
				parentRef.Resource = "serviceentries"
				parentRef.Name = opts.localServiceEntry
			}

			resource := &networkingv1.IPAddress{
				ObjectMeta: metav1.ObjectMeta{
					Name:   address,
					Labels: labels,
				},
				Spec: networkingv1.IPAddressSpec{
					ParentRef: parentRef,
				},
			}
			if _, err := client.Kube().NetworkingV1().IPAddresses().Create(cmd.Context(), resource, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("creating IPAddress %s: %v", address, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ipaddress %s created\n", address)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.serviceName, "service-name", "", "Original name of the Service")
	cmd.Flags().StringVar(&opts.originNS, "origin-ns", "", "Original namespace of the Service")
	cmd.Flags().StringVar(&opts.localServiceName, "local-service-name", "", "Name of the local parent Service")
	cmd.Flags().StringVar(&opts.localServiceEntry, "local-service-entry", "", "Name of the local parent ServiceEntry")
	cmd.Flags().StringVar(&opts.localNS, "local-ns", "", "Namespace of the local parent resource")

	return cmd
}
