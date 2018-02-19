// Copyright 2017 Istio Authors.
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

package convert

import (
	"fmt"
	"io/ioutil"
	"os"
	"io"

	"github.com/ghodss/yaml"
	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

var (
	inFilenames []string
	outFilename string
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "convert",
		Short:   "Convert configs from v1alpha1 to v1alpha2",
		Long:    "Disclaimer disclaimer disclaimer",
		Example: "",
		RunE: func(c *cobra.Command, args []string) error {
			configDescriptor := model.ConfigDescriptor{
				model.RouteRule,
				model.V1alpha2RouteRule,
				model.Gateway,
				model.EgressRule,
				model.ExternalService,
				model.DestinationPolicy,
				model.DestinationRule,
				model.HTTPAPISpec,
				model.HTTPAPISpecBinding,
				model.QuotaSpec,
				model.QuotaSpecBinding,
				model.EndUserAuthenticationPolicySpec,
				model.EndUserAuthenticationPolicySpecBinding,
			}

			if len(inFilenames) == 0 {
				return fmt.Errorf("No input files provided")
			}

			readers := make([]io.Reader, 0)
			for _, filename := range inFilenames {
				if filename == "-" {
					readers = append(readers, os.Stdin)
				} else {
					file, err := os.Open(filename)
					if err != nil {
						return err
					}
					defer func() {
						if err := file.Close(); err != nil {
							log.Errorf("Did not close input %s successfully: %v",
								filename, err)
						}
					}()
					readers = append(readers, file)
				}
			}

			writer := os.Stdout
			if outFilename != "-" {
				file, err := os.Create(outFilename)
				if err != nil {
					return err
				}
				defer func() {
					if err := file.Close(); err != nil {
						log.Errorf("Did not close output successfully: %v", err)
					}
				}()

				writer = file
			}

			configs, err := readConfigs(readers)
			if err != nil {
				return err
			}

			if err := validateConfigs(configs); err != nil {
				return err
			}

			out := make([]model.Config, 0)
			out = append(out, convertDestinationPolicies(configs)...)
			out = append(out, convertRouteRules(configs)...)
			out = append(out, convertEgressRules(configs)...)
			// TODO: k8s ingress -> gateway?
			// TODO: create missing destination rules/subsets?

			writeYAMLOutput(configDescriptor, out, writer)

			// sanity check that the outputs are valid
			if err := validateConfigs(out); err != nil {
				log.Warnf("Output config(s) are invalid: %v", err)
			}

			return nil
		},
	}

	cmd.PersistentFlags().StringSliceVarP(&inFilenames, "filenames", "f",
		nil, "Input filename")
	cmd.PersistentFlags().StringVarP(&outFilename, "output", "o",
		"-", "Output filename")

	return cmd
}

func readConfigs(readers []io.Reader) ([]model.Config, error) {
	out := make([]model.Config, 0)
	for _, reader := range readers {
		data, err := ioutil.ReadAll(reader)
		if err != nil {
			return nil, err
		}

		configs, kinds, err := crd.ParseInputs(string(data))
		if err != nil {
			return nil, err
		}
		if len(kinds) != 0 {
			return nil, fmt.Errorf("Unsupported kinds: %v", kinds)
		}

		out = append(out, configs...)
	}
	return out, nil
}

func writeYAMLOutput(descriptor model.ConfigDescriptor, configs []model.Config, writer io.Writer) {
	for i, config := range configs {
		schema, exists := descriptor.GetByType(config.Type)
		if !exists {
			log.Errorf("Unknown kind %q for %v", crd.ResourceName(config.Type), config.Name)
			continue
		}
		obj, err := crd.ConvertConfig(schema, config)
		if err != nil {
			log.Errorf("Could not decode %v: %v", config.Name, err)
			continue
		}
		bytes, err := yaml.Marshal(obj)
		if err != nil {
			log.Errorf("Could not convert %v to YAML: %v", config, err)
			continue
		}
		writer.Write(bytes)
		if i+1 < len(configs) {
			writer.Write([]byte("---"))
		}
	}
}

func validateConfigs(configs []model.Config) (errs error) {
	for _, config := range configs {
		var err error
		switch config.Type {
		case model.RouteRule.Type:
			err = model.ValidateRouteRule(config.Spec)
		case model.V1alpha2RouteRule.Type:
			err = model.ValidateRouteRuleV2(config.Spec)
		case model.Gateway.Type:
			err = model.ValidateGateway(config.Spec)
		case model.EgressRule.Type:
			err = model.ValidateEgressRule(config.Spec)
		case model.ExternalService.Type:
			err = model.ValidateExternalService(config.Spec)
		case model.DestinationPolicy.Type:
			err = model.ValidateDestinationPolicy(config.Spec)
		case model.DestinationRule.Type:
			err = model.ValidateDestinationRule(config.Spec)
		}
		if err != nil {
			multierror.Append(err, errs)
		}
	}
	return
}
