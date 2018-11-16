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
	"errors"
	"fmt"
	"strings"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"istio.io/istio/istioctl/pkg/genbinding"
	"istio.io/istio/pilot/pkg/model"
)

const defaultEgressGateway = "istio-egressgateway.istio-system"

var (
	remoteClusters []string
	addressLabels  string
	useEgress      bool
	egressGateway  string

	genBindingCmd = &cobra.Command{
		Use:     "gen-binding",
		Short:   "Generate Istio traffic routing configuration to direct traffic to another Istio mesh",
		Long:    "Generate Istio traffic routing configuration to direct traffic to another Istio mesh.",
		Example: "istioctl experimental gen-binding <service:port> --cluster <ip:port> [--cluster <ip:port>]* [--labels key1=value1,key2=value2] [--use-egress] [--egressgateway <ip:port>]", // nolint: lll
		RunE: func(c *cobra.Command, args []string) error {

			if len(args) != 1 {
				return fmt.Errorf("usage: %s", c.Example)
			}

			if len(remoteClusters) == 0 {
				return fmt.Errorf("usage: %s", c.Example)
			}

			labels, err := parseLabels(addressLabels)
			if err != nil {
				return multierror.Prefix(err, "could not parse --labels")
			}

			if len(namespace) == 0 {
				namespace = defaultNamespace
			}

			if explicitlySet(c.Flags(), "use-egress") && !useEgress && egressGateway != defaultEgressGateway {
				return errors.New("cannot combine --use-egress=false and --egressgateway")
			}
			if egressGateway != defaultEgressGateway {
				useEgress = true
			}
			if !useEgress {
				egressGateway = ""
			}

			bindings, err := genbinding.CreateBinding(args[0], remoteClusters, labels, egressGateway, namespace)
			if err != nil {
				return multierror.Prefix(err, "could not create binding:")
			}

			configDescriptor := model.ConfigDescriptor{
				model.ServiceEntry,
			}
			writeYAMLOutput(configDescriptor, bindings, c.OutOrStdout())

			// sanity check that the outputs are valid
			if err := validateConfigs(bindings); err != nil {
				return multierror.Prefix(err, "output config(s) are invalid:")
			}
			return nil
		},
	}
)

func init() {
	genBindingCmd.PersistentFlags().StringSliceVarP(&remoteClusters, "cluster", "",
		nil, "IP:Port of remote Istio Ingress")
	genBindingCmd.PersistentFlags().StringVarP(&addressLabels, "labels", "",
		"", "key=value pairs for subsets")
	genBindingCmd.PersistentFlags().BoolVarP(&useEgress, "use-egress", "",
		false, "Use Egress Gateway")
	genBindingCmd.PersistentFlags().StringVarP(&egressGateway, "egressgateway", "",
		defaultEgressGateway, "Egress Gateway Address")

	experimentalCmd.AddCommand(genBindingCmd)
}

// parseLabels() creatse a map from strings like "key1=value1,key2=value2"
func parseLabels(s string) (map[string]string, error) {
	retval := make(map[string]string)
	for _, entry := range strings.Split(s, ",") {
		if entry != "" {
			sides := strings.Split(entry, "=")
			if len(sides) < 2 {
				return nil, fmt.Errorf("missing =")
			}
			retval[sides[0]] = strings.Join(sides[1:], "=")
		}
	}
	return retval, nil
}

func explicitlySet(fs *pflag.FlagSet, s string) bool {
	retval := false
	fs.Visit(func(f *pflag.Flag) {
		retval = retval || (f.Name == s)
	})
	return retval
}
