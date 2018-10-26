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

	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/istioctl/pkg/genbinding"
)

var (
	remoteClusters []string
	remoteSubset   string
	genBindingCmd  = &cobra.Command{
		Use:     "gen-binding",
		Short:   "Generate Istio Configuration to direct traffic to another Istio mesh",
		Long:    "Generate Istio Configuration to direct traffic to another Istio mesh.",
		Example: "istioctl experimental gen-binding <service:port> --cluster <ip:port> [--cluster <ip:port>]* [--subset <subset>]",
		RunE: func(c *cobra.Command, args []string) error {
			// fmt.Fprintf(c.OutOrStdout(), "Args is %#v\n", args)                     // TODO remove
			// fmt.Fprintf(c.OutOrStdout(), "remoteClusters is %#v\n", remoteClusters) // TODO remove
			// fmt.Fprintf(c.OutOrStdout(), "remoteSubset is %#v\n", remoteSubset)     // TODO remove

			if len(args) != 1 {
				return fmt.Errorf("Usage: %s", c.Example)
			}

			if len(remoteClusters) == 0 {
				return fmt.Errorf("Usage: %s", c.Example)
			}

			bindings, svcs, err := genbinding.CreateBinding(args[0], remoteClusters, remoteSubset)
			if err != nil {
				return multierror.Prefix(err, "could not create binding:")
			}
			_ = svcs // TODO if we need to generate K8s services, we need to write them out.  Currently we neither gen nor write them.

			configDescriptor := model.ConfigDescriptor{
				model.VirtualService,
				model.DestinationRule,
			}
			writeYAMLOutput(configDescriptor, bindings, c.OutOrStdout())

			// sanity check that the outputs are valid
			if err := validateConfigs(bindings); err != nil {
				return multierror.Prefix(err, "output config(s) are invalid:")
			}
			return nil

			return nil
		},
	}
)

func init() {
	genBindingCmd.PersistentFlags().StringSliceVarP(&remoteClusters, "cluster", "",
		nil, "IP:Port of remote Istio Ingress")
	genBindingCmd.PersistentFlags().StringVarP(&remoteSubset, "subset", "",
		"-", "Subset")

	experimentalCmd.AddCommand(genBindingCmd)
}
