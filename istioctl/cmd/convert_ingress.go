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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/ghodss/yaml"
	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"k8s.io/api/networking/v1beta1"
	"k8s.io/client-go/kubernetes"

	"istio.io/pkg/log"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/convert"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/validation"
)

var (
	inFilenames        []string
	outConvertFilename string
)

func convertConfigs(readers []io.Reader, writer io.Writer, client kubernetes.Interface) error {
	configs, ingresses, err := readConfigs(readers)
	if err != nil {
		return err
	}

	if err = validateConfigs(configs); err != nil {
		return err
	}

	// ingresses without specified namespace need to generate valid output; use the default
	for _, ingress := range ingresses {
		if ingress.Namespace == "" {
			ingress.Namespace = defaultNamespace
		}
	}

	out := make([]model.Config, 0)
	convertedIngresses, err := convert.IstioIngresses(ingresses, "", client)
	if err == nil {
		out = append(out, convertedIngresses...)
	} else {
		return multierror.Prefix(err, "Ingress rules invalid")
	}

	writeYAMLOutput(out, writer)

	// sanity check that the outputs are valid
	if err := validateConfigs(out); err != nil {
		return multierror.Prefix(err, "output config(s) are invalid:")
	}
	return nil
}

func readConfigs(readers []io.Reader) ([]model.Config, []*v1beta1.Ingress, error) {
	out := make([]model.Config, 0)
	outIngresses := make([]*v1beta1.Ingress, 0)

	for _, reader := range readers {
		data, err := ioutil.ReadAll(reader)
		if err != nil {
			return nil, nil, err
		}

		configs, kinds, err := crd.ParseInputs(string(data))
		if err != nil {
			return nil, nil, err
		}

		recognized := 0
		for _, nonIstio := range kinds {
			if nonIstio.Kind == "Ingress" &&
				(nonIstio.APIVersion == "extensions/v1beta1" ||
					nonIstio.APIVersion == "networking.k8s.io/v1beta1") {

				ingress, err := parseIngress(nonIstio)
				if err != nil {
					log.Errorf("Could not decode ingress %v: %v", nonIstio.Name, err)
					continue
				}

				outIngresses = append(outIngresses, ingress)
				recognized++
			}
		}

		if len(kinds) > recognized {
			// If convert-networking-config was asked to convert non-network things,
			// like Deployments and Services, return a brief informative error
			kindsFound := make(map[string]bool)
			for _, kind := range kinds {
				kindsFound[kind.Kind] = true
			}

			var msg error
			for kind := range kindsFound {
				msg = multierror.Append(msg, fmt.Errorf("unsupported kind: %v", kind))
			}

			return nil, nil, msg
		}

		out = append(out, configs...)
	}
	return out, outIngresses, nil
}

func writeYAMLOutput(configs []model.Config, writer io.Writer) {
	for i, cfg := range configs {
		obj, err := crd.ConvertConfig(cfg)
		if err != nil {
			log.Errorf("Could not decode %v: %v", cfg.Name, err)
			continue
		}
		bytes, err := yaml.Marshal(obj)
		if err != nil {
			log.Errorf("Could not convert %v to YAML: %v", cfg, err)
			continue
		}
		_, _ = writer.Write(bytes) // nolint: errcheck
		if i+1 < len(configs) {
			_, _ = writer.Write([]byte("---\n")) // nolint: errcheck
		}
	}
}

func validateConfigs(configs []model.Config) error {
	var errs error
	for _, cfg := range configs {
		if collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind() == cfg.GroupVersionKind {
			if err := validation.ValidateVirtualService(cfg.Name, cfg.Namespace, cfg.Spec); err != nil {
				errs = multierror.Append(err, errs)
			}
		}
	}
	return errs
}

func parseIngress(unparsed crd.IstioKind) (*v1beta1.Ingress, error) {
	// To convert unparsed to a v1beta1.Ingress Marshal into JSON and Unmarshal back
	b, err := json.Marshal(unparsed)
	if err != nil {
		return nil, multierror.Prefix(err, "can't reserialize Ingress")
	}

	out := &v1beta1.Ingress{}
	err = json.Unmarshal(b, out)
	if err != nil {
		return nil, multierror.Prefix(err, "can't deserialize as Ingress")
	}

	return out, nil
}

func convertIngress() *cobra.Command {
	convertIngressCmd := &cobra.Command{
		Use:   "convert-ingress",
		Short: "Convert Ingress configuration into Istio VirtualService configuration",
		Long: "Converts Ingresses into VirtualService configuration on a best effort basis. " +
			"The output should be considered a starting point for your Istio configuration and probably " +
			"require some minor modification. " +
			"Warnings will be generated where configs cannot be converted perfectly. " +
			"The input must be a Kubernetes Ingress. " +
			"The conversion of v1alpha1 Istio rules has been removed from istioctl.",
		Example: "istioctl convert-ingress -f samples/bookinfo/platform/kube/bookinfo-ingress.yaml",
		RunE: func(c *cobra.Command, args []string) error {
			var opts clioptions.ControlPlaneOptions
			client, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			if len(inFilenames) == 0 {
				return fmt.Errorf("no input files provided")
			}

			readers := make([]io.Reader, 0)
			if len(inFilenames) == 1 && inFilenames[0] == "-" {
				readers = append(readers, os.Stdin)
			} else {
				for _, filename := range inFilenames {
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
			if outConvertFilename != "-" {
				file, err := os.Create(outConvertFilename)
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

			return convertConfigs(readers, writer, client)
		},
	}

	convertIngressCmd.PersistentFlags().StringSliceVarP(&inFilenames, "filenames", "f",
		nil, "Input filenames")
	convertIngressCmd.PersistentFlags().StringVarP(&outConvertFilename, "output", "o",
		"-", "Output filename")

	return convertIngressCmd
}
