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
	"github.com/spf13/cobra"

	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/log"
)

var (
	deregisterCmd = &cobra.Command{
		Use:              "deregister <svcname> <ip>",
		Short:            "De-registers a service instance",
		Args:             cobra.MinimumNArgs(2),
		PersistentPreRun: getRealKubeConfig,
		RunE: func(c *cobra.Command, args []string) error {
			svcName := args[0]
			ip := args[1]
			log.Infof("De-registering for service '%s' ip '%s'",
				svcName, ip)
			_, client, err := kube.CreateInterface(kubeconfig)
			if err != nil {
				return err
			}
			return kube.DeRegisterEndpoint(client, namespace, svcName, ip)
		},
	}
)

func init() {
	rootCmd.AddCommand(deregisterCmd)
}
