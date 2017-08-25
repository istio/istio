// Copyright 2017 Istio Authors
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

	"github.com/golang/glog"
	"github.com/spf13/cobra"

	"istio.io/pilot/platform/kube"
)

var (
	registerCmd = &cobra.Command{
		Use:   "register <svcname> <ip> [name1:]port1 [name2:]port2 ...",
		Short: "Registers a service instance (e.g. VM) joining the mesh",
		Args:  cobra.MinimumNArgs(3),
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
			glog.Infof("Registering for service '%s' ip '%s', ports list %v",
				svcName, ip, portsList)
			if svcAcctAnn != "" {
				annotations = append(annotations, fmt.Sprintf("%s=%s", kube.KubeServiceAccountsOnVMAnnotation, svcAcctAnn))
			}
			glog.Infof("%d labels (%v) and %d annotations (%v)",
				len(labels), labels, len(annotations), annotations)
			client, err := kube.CreateInterface(kubeconfig)
			if err != nil {
				return err
			}
			return kube.RegisterEndpoint(client, namespace, svcName, ip, portsList, labels, annotations)
		},
	}
	labels      []string
	annotations []string
	svcAcctAnn  string
)

func init() {
	rootCmd.AddCommand(registerCmd)
	registerCmd.PersistentFlags().StringSliceVarP(&labels, "labels", "l",
		nil, "List of labels to apply if creating a service/endpoint; e.g. -l env=prod,vers=2")
	registerCmd.PersistentFlags().StringSliceVarP(&annotations, "annotations", "a",
		nil, "List of string annotations to apply if creating a service/endpoint; e.g. -a foo=bar,test,x=y")
	registerCmd.PersistentFlags().StringVarP(&svcAcctAnn, "serviceaccount", "s",
		"default", "Service account to link to the service")
}
