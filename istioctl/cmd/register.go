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
	"fmt"

	"github.com/spf13/cobra"

	"istio.io/api/annotation"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/pkg/log"
)

var (
	labels      []string
	annotations []string
	svcAcctAnn  string
)

func register() *cobra.Command {
	registerCmd := &cobra.Command{
		Use:   "register <svcname> <ip> [name1:]port1 [name2:]port2 ...",
		Short: "Registers a service instance (e.g. VM) joining the mesh",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 3 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("register requires service name, IP, and port")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			svcName := args[0]
			ip := args[1]
			portsListStr := args[2:]
			portsList := make([]kube.NamedPort, len(portsListStr))
			for i := range portsListStr {
				p, err := kube.Str2NamedPort(portsListStr[i])
				if err != nil {
					return err
				}
				portsList[i] = p
			}
			log.Infof("Registering for service '%s' ip '%s', ports list %v",
				svcName, ip, portsList)
			if svcAcctAnn != "" {
				annotations = append(annotations,
					fmt.Sprintf("%s=%s", annotation.AlphaKubernetesServiceAccounts.Name, svcAcctAnn))
			}
			log.Infof("%d labels (%v) and %d annotations (%v)",
				len(labels), labels, len(annotations), annotations)
			client, err := createInterface(kubeconfig)
			if err != nil {
				return err
			}
			ns := handlers.HandleNamespace(namespace, defaultNamespace)
			return kube.RegisterEndpoint(client, ns, svcName, ip, portsList, labels, annotations)
		},
	}

	registerCmd.PersistentFlags().StringSliceVarP(&labels, "labels", "l",
		nil, "List of labels to apply if creating a service/endpoint; e.g. -l env=prod,vers=2")
	registerCmd.PersistentFlags().StringSliceVarP(&annotations, "annotations", "a",
		nil, "List of string annotations to apply if creating a service/endpoint; e.g. -a foo=bar,test,x=y")
	registerCmd.PersistentFlags().StringVarP(&svcAcctAnn, "serviceaccount", "s",
		"default", "Service account to link to the service")

	return registerCmd
}
