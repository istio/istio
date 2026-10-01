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

package service

import (
	"fmt"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/istio/istioctl/pkg/cli"
)

type createOptions struct {
	port        int32
	selectors   map[string]string
	serviceType string
	serviceName string
	originNS    string
	localNS     string
}

const (
	originalServiceNameLabel      = "istio.io/original-service-name"
	originalServiceNamespaceLabel = "istio.io/original-service-namespace"
)

func Cmd(ctx cli.Context) *cobra.Command {
	serviceCmd := &cobra.Command{
		Use:   "sentinel-service",
		Short: "Manage Kubernetes Services",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	serviceCmd.AddCommand(createCmd(ctx))
	return serviceCmd
}

func createCmd(ctx cli.Context) *cobra.Command {
	opts := createOptions{}
	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a Kubernetes Service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.port < 1 || opts.port > 65535 {
				return fmt.Errorf("port must be between 1 and 65535")
			}

			serviceType := corev1.ServiceType(opts.serviceType)
			switch serviceType {
			case corev1.ServiceTypeClusterIP, corev1.ServiceTypeNodePort, corev1.ServiceTypeLoadBalancer:
			default:
				return fmt.Errorf("unsupported service type %q", opts.serviceType)
			}

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

			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      args[0],
					Namespace: namespace,
					Labels:    labels,
				},
				Spec: corev1.ServiceSpec{
					Selector: opts.selectors,
					Type:     serviceType,
					Ports: []corev1.ServicePort{{
						Port:       opts.port,
						TargetPort: intstr.FromInt32(opts.port),
						Protocol:   corev1.ProtocolTCP,
					}},
				},
			}

			if _, err := client.Kube().CoreV1().Services(namespace).Create(cmd.Context(), svc, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("creating service %s/%s: %v", namespace, svc.Name, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "service %s/%s created\n", namespace, svc.Name)
			return nil
		},
	}

	cmd.Flags().Int32Var(&opts.port, "port", 0, "Port exposed by the Service")
	cmd.Flags().StringToStringVarP(&opts.selectors, "selector", "l", nil, "Selector identifying Pods for the Service")
	cmd.Flags().StringVar(&opts.serviceType, "type", string(corev1.ServiceTypeClusterIP), "Service type: ClusterIP, NodePort, or LoadBalancer")
	cmd.Flags().StringVar(&opts.serviceName, "service-name", "", "Original name of the Service")
	cmd.Flags().StringVar(&opts.originNS, "origin-ns", "", "Original namespace of the Service")
	cmd.Flags().StringVar(&opts.localNS, "local-ns", "", "Namespace where the Service is created")
	_ = cmd.MarkFlagRequired("port")

	return cmd
}
