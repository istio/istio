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
	"io"
	"io/ioutil"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"

	"istio.io/pilot/adapter/config/crd"
	"istio.io/pilot/cmd"
	"istio.io/pilot/model"
	"istio.io/pilot/platform/kube"
	"istio.io/pilot/tools/version"
)

const (
	kubePlatform = "kube"
)

var (
	platform string

	kubeconfig     string
	namespace      string
	istioNamespace string

	// input file name
	file string

	// output format (yaml or short)
	outputFormat string

	rootCmd = &cobra.Command{
		Use:               "istioctl",
		Short:             "Istio control interface",
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		Long: fmt.Sprintf(`
Istio configuration command line utility.

Create, list, modify, and delete configuration resources in the Istio
system.

Available routing and traffic management configuration types:

	%v

See http://istio.io/docs/reference for an overview of routing rules
and destination policies.

`, model.IstioConfigTypes.Types()),
	}

	postCmd = &cobra.Command{
		Use:   "create",
		Short: "Create policies and rules",
		Example: `
			istioctl create -f example-routing.yaml
			`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 0 {
				c.Println(c.UsageString())
				return fmt.Errorf("create takes no arguments")
			}
			varr, err := readInputs()
			if err != nil {
				return err
			}
			if len(varr) == 0 {
				return errors.New("nothing to create")
			}
			for _, config := range varr {
				if config.Namespace == "" {
					config.Namespace = namespace
				}

				configClient, err := newClient()
				if err != nil {
					return err
				}
				rev, err := configClient.Create(config)
				if err != nil {
					return err
				}
				fmt.Printf("Created config %v at revision %v\n", config.Key(), rev)
			}

			return nil
		},
	}

	putCmd = &cobra.Command{
		Use:   "replace",
		Short: "Replace existing policies and rules",
		Example: `
			istioctl replace -f example-routing.yaml
			`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 0 {
				c.Println(c.UsageString())
				return fmt.Errorf("replace takes no arguments")
			}
			varr, err := readInputs()
			if err != nil {
				return err
			}
			if len(varr) == 0 {
				return errors.New("nothing to replace")
			}
			for _, config := range varr {
				if config.Namespace == "" {
					config.Namespace = namespace
				}

				configClient, err := newClient()
				if err != nil {
					return err
				}
				// fill up revision
				if config.ResourceVersion == "" {
					current, exists := configClient.Get(config.Type, config.Name, config.Namespace)
					if exists {
						config.ResourceVersion = current.ResourceVersion
					}
				}

				newRev, err := configClient.Update(config)
				if err != nil {
					return err
				}

				fmt.Printf("Updated config %v to revision %v\n", config.Key(), newRev)
			}

			return nil
		},
	}

	getCmd = &cobra.Command{
		Use:   "get <type> [<name>]",
		Short: "Retrieve policies and rules",
		Example: `
		# List all route rules
		istioctl get routerules

		# List all destination policies
		istioctl get destinationpolicies

		# Get a specific rule named productpage-default
		istioctl get routerule productpage-default
		`,
		RunE: func(c *cobra.Command, args []string) error {
			configClient, err := newClient()
			if err != nil {
				return err
			}
			if len(args) < 1 {
				c.Println(c.UsageString())
				return fmt.Errorf("specify the type of resource to get. Types are %v",
					strings.Join(supportedTypes(configClient), ", "))
			}

			typ, err := schema(configClient, args[0])
			if err != nil {
				c.Println(c.UsageString())
				return err
			}

			var configs []model.Config
			if len(args) > 1 {
				config, exists := configClient.Get(typ.Type, args[1], namespace)
				if exists {
					configs = append(configs, *config)
				}
			} else {
				configs, err = configClient.List(typ.Type, namespace)
				if err != nil {
					return err
				}
			}

			if len(configs) == 0 {
				fmt.Println("No resources found.")
				return nil
			}

			var outputters = map[string](func(*crd.Client, []model.Config)){
				"yaml":  printYamlOutput,
				"short": printShortOutput,
			}

			if outputFunc, ok := outputters[outputFormat]; ok {
				outputFunc(configClient, configs)
			} else {
				return fmt.Errorf("unknown output format %v. Types are yaml|short", outputFormat)
			}

			return nil
		},
	}

	deleteCmd = &cobra.Command{
		Use:   "delete <type> <name> [<name2> ... <nameN>]",
		Short: "Delete policies or rules",
		Example: `
		# Delete a rule using the definition in example-routing.yaml.
		istioctl delete -f example-routing.yaml

		# Delete the rule productpage-default
		istioctl delete routerule productpage-default
		`,
		RunE: func(c *cobra.Command, args []string) error {
			configClient, errs := newClient()
			if errs != nil {
				return errs
			}
			// If we did not receive a file option, get names of resources to delete from command line
			if file == "" {
				if len(args) < 2 {
					c.Println(c.UsageString())
					return fmt.Errorf("provide configuration type and name or -f option")
				}
				typ, err := schema(configClient, args[0])
				if err != nil {
					return err
				}
				for i := 1; i < len(args); i++ {
					if err := configClient.Delete(typ.Type, args[i], namespace); err != nil {
						errs = multierror.Append(errs,
							fmt.Errorf("cannot delete %s: %v", args[i], err))
					} else {
						fmt.Printf("Deleted config: %v %v\n", args[0], args[i])
					}
				}
				return errs
			}

			// As we did get a file option, make sure the command line did not include any resources to delete
			if len(args) != 0 {
				c.Println(c.UsageString())
				return fmt.Errorf("delete takes no arguments when the file option is used")
			}
			varr, err := readInputs()
			if err != nil {
				return err
			}
			if len(varr) == 0 {
				return errors.New("nothing to delete")
			}
			for _, config := range varr {
				if config.Namespace == "" {
					config.Namespace = namespace
				}

				// compute key if necessary
				if err = configClient.Delete(config.Type, config.Name, config.Namespace); err != nil {
					errs = multierror.Append(errs, fmt.Errorf("cannot delete %s: %v", config.Key(), err))
				} else {
					fmt.Printf("Deleted config: %v\n", config.Key())
				}
			}
			return errs
		},
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Display version information",
		RunE: func(c *cobra.Command, args []string) error {
			fmt.Println(version.Version())
			return nil
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&platform, "platform", "p", kubePlatform,
		"Istio host platform")
	defaultKubeconfig := os.Getenv("HOME") + "/.kube/config"
	if v := os.Getenv("KUBECONFIG"); v != "" {
		defaultKubeconfig = v
	}
	rootCmd.PersistentFlags().StringVarP(&kubeconfig, "kubeconfig", "c", defaultKubeconfig,
		"Kubernetes configuration file")

	rootCmd.PersistentFlags().StringVarP(&istioNamespace, "istioNamespace", "i", kube.IstioNamespace,
		"Istio system namespace")

	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", v1.NamespaceDefault,
		"Config namespace")

	postCmd.PersistentFlags().StringVarP(&file, "file", "f", "",
		"Input file with the content of the configuration objects (if not set, command reads from the standard input)")
	putCmd.PersistentFlags().AddFlag(postCmd.PersistentFlags().Lookup("file"))
	deleteCmd.PersistentFlags().AddFlag(postCmd.PersistentFlags().Lookup("file"))

	getCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "short",
		"Output format. One of:yaml|short")

	cmd.AddFlags(rootCmd)

	rootCmd.AddCommand(postCmd)
	rootCmd.AddCommand(putCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if platform != kubePlatform {
		glog.Warningf("Platform '%s' not supported.", platform)
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}

// The schema is based on the kind (for example "routerule" or "destinationpolicy")
func schema(configClient *crd.Client, typ string) (model.ProtoSchema, error) {
	for _, desc := range configClient.ConfigDescriptor() {
		switch typ {
		case desc.Type, desc.Plural: // legacy hyphenated resources names
			return model.ProtoSchema{}, fmt.Errorf("%q not recognized. Please use non-hyphenated resource name %q",
				typ, crd.ResourceName(typ))
		case crd.ResourceName(desc.Type), crd.ResourceName(desc.Plural):
			return desc, nil
		}
	}
	return model.ProtoSchema{}, fmt.Errorf("Istio doesn't have configuration type %s, the types are %v",
		typ, strings.Join(supportedTypes(configClient), ", "))
}

// readInputs reads multiple documents from the input and checks with the schema
func readInputs() ([]model.Config, error) {
	var reader io.Reader
	switch file {
	case "":
		return nil, errors.New("filename not specified (see --filename or -f)")
	case "-":
		reader = os.Stdin
	default:
		var err error
		if reader, err = os.Open(file); err != nil {
			return nil, err
		}
	}
	input, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return crd.ParseInputs(string(input))
}

// Print a simple list of names
func printShortOutput(_ *crd.Client, configList []model.Config) {
	var w tabwriter.Writer
	w.Init(os.Stdout, 0, 8, 0, '\t', 0)
	fmt.Fprintf(&w, "NAME\tKIND\tNAMESPACE\n")
	for _, c := range configList {
		kind := fmt.Sprintf("%s.%s.%s",
			crd.KabobCaseToCamelCase(c.Type),
			model.IstioAPIVersion,
			model.IstioAPIGroup,
		)
		fmt.Fprintf(&w, "%s\t%s\t%s\n", c.Name, kind, c.Namespace)
	}
	w.Flush() // nolint: errcheck
}

// Print as YAML
func printYamlOutput(configClient *crd.Client, configList []model.Config) {
	descriptor := configClient.ConfigDescriptor()
	for _, config := range configList {
		schema, exists := descriptor.GetByType(config.Type)
		if !exists {
			glog.Errorf("Unknown kind %q for %v", crd.ResourceName(config.Type), config.Name)
			continue
		}
		obj, err := crd.ConvertConfig(schema, config)
		if err != nil {
			glog.Errorf("Could not decode %v: %v", config.Name, err)
			continue
		}
		bytes, err := yaml.Marshal(obj)
		if err != nil {
			glog.Errorf("Could not convert %v to YAML: %v", config, err)
			continue
		}
		fmt.Print(string(bytes))
		fmt.Println("---")
	}
}

func newClient() (*crd.Client, error) {
	return crd.NewClient(kubeconfig, model.ConfigDescriptor{
		model.RouteRule,
		model.EgressRule,
		model.DestinationPolicy,
	}, "")
}

func supportedTypes(configClient *crd.Client) []string {
	types := configClient.ConfigDescriptor().Types()
	for i := range types {
		types[i] = crd.ResourceName(types[i])
	}
	return types
}
