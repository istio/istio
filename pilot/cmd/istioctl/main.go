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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"k8s.io/client-go/pkg/api"

	"istio.io/manager/cmd"
	"istio.io/manager/cmd/version"
	"istio.io/manager/model"
	"istio.io/manager/platform/kube"

	"k8s.io/client-go/pkg/util/yaml"
)

// Each entry in the multi-doc YAML file used by `istioctl create -f` MUST have this format
type inputDoc struct {
	// Type SHOULD be one of the kinds in model.IstioConfig; a route-rule, ingress-rule, or destination-policy
	Type string      `json:"type,omitempty"`
	Name string      `json:"name,omitempty"`
	Spec interface{} `json:"spec,omitempty"`
	// ParsedSpec will be one of the messages in model.IstioConfig: for example an
	// istio.proxy.v1alpha.config.RouteRule or DestinationPolicy
	ParsedSpec proto.Message `json:"-"`
}

var (
	kubeconfig string
	namespace  string
	config     model.ConfigRegistry

	// input file name
	file string

	// output format (yaml or short)
	outputFormat string

	key    model.Key
	schema model.ProtoSchema

	rootCmd = &cobra.Command{
		Use:   "istioctl",
		Short: "Istio control interface",
		Long: fmt.Sprintf("Istio configuration command line utility. Available configuration types: %v",
			model.IstioConfig.Kinds()),
		PersistentPreRunE: func(*cobra.Command, []string) (err error) {
			if kubeconfig == "" {
				if v := os.Getenv("KUBECONFIG"); v != "" {
					glog.V(2).Infof("Setting configuration from KUBECONFIG environment variable")
					kubeconfig = v
				}
			}

			client, err := kube.NewClient(kubeconfig, model.IstioConfig)
			if err != nil && kubeconfig == "" {
				// If no configuration was specified, and the platform client failed, try again using ~/.kube/config
				client, err = kube.NewClient(os.Getenv("HOME")+"/.kube/config", model.IstioConfig)
			}
			if err != nil {
				return multierror.Prefix(err, "failed to connect to Kubernetes API.")
			}

			if err = client.RegisterResources(); err != nil {
				return multierror.Prefix(err, "failed to register Third-Party Resources.")
			}

			config = client
			return
		},
	}

	postCmd = &cobra.Command{
		Use:   "create",
		Short: "Create policies and rules",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("create takes no arguments")
			}
			varr, err := readInputs()
			if err != nil {
				return err
			}
			if len(varr) == 0 {
				return errors.New("nothing to create")
			}
			for _, v := range varr {
				if err = setup(v.Type, v.Name); err != nil {
					return err
				}
				err = config.Post(key, v.ParsedSpec)
				if err != nil {
					return err
				}
				fmt.Printf("Posted %v %v\n", v.Type, v.Name)
			}

			return nil
		},
	}

	putCmd = &cobra.Command{
		Use:   "replace",
		Short: "Replace policies and rules",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("replace takes no arguments")
			}
			varr, err := readInputs()
			if err != nil {
				return err
			}
			if len(varr) == 0 {
				return errors.New("nothing to replace")
			}
			for _, v := range varr {
				if err = setup(v.Type, v.Name); err != nil {
					return err
				}
				err = config.Put(key, v.ParsedSpec)
				if err != nil {
					return err
				}
				fmt.Printf("Put %v %v\n", v.Type, v.Name)
			}

			return nil
		},
	}

	getCmd = &cobra.Command{
		Use:   "get <type> <name>",
		Short: "Retrieve a policy or rule",
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("specify the type of resource to get. Types are %v",
					strings.Join(model.IstioConfig.Kinds(), ", "))
			}

			if len(args) > 1 {
				if err := setup(args[0], args[1]); err != nil {
					return err
				}
				item, exists := config.Get(key)
				if !exists {
					return fmt.Errorf("%q does not exist", key)
				}
				out, err := schema.ToYAML(item)
				if err != nil {
					return err
				}
				fmt.Print(out)
			} else {
				if err := setup(args[0], ""); err != nil {
					return err
				}

				list, err := config.List(key.Kind, key.Namespace)
				if err != nil {
					return fmt.Errorf("error listing %s: %v", key.Kind, err)
				}

				var outputters = map[string](func(map[model.Key]proto.Message) error){
					"yaml":  printYamlOutput,
					"short": printShortOutput,
				}
				if outputFunc, ok := outputters[outputFormat]; ok {
					if err := outputFunc(list); err != nil {
						return err
					}
				} else {
					return fmt.Errorf("unknown output format %v. Types are yaml|short", outputFormat)
				}

			}

			return nil
		},
	}

	deleteCmd = &cobra.Command{
		Use:   "delete <type> <name> [<name2> ... <nameN>]",
		Short: "Delete policies or rules",
		RunE: func(c *cobra.Command, args []string) error {
			// If we did not receive a file option, get names of resources to delete from command line
			if file == "" {
				if len(args) < 2 {
					return fmt.Errorf("provide configuration type and name or -f option")
				}
				for i := 1; i < len(args); i++ {
					if err := setup(args[0], args[i]); err != nil {
						return err
					}
					if err := config.Delete(key); err != nil {
						return err
					}
					fmt.Printf("Deleted %v %v\n", args[0], args[i])
				}
				return nil
			}

			// As we did get a file option, make sure the command line did not include any resources to delete
			if len(args) != 0 {
				return fmt.Errorf("delete takes no arguments when the file option is used")
			}
			varr, err := readInputs()
			if err != nil {
				return err
			}
			if len(varr) == 0 {
				return errors.New("nothing to delete")
			}
			for _, v := range varr {
				if err = setup(v.Type, v.Name); err != nil {
					return err
				}
				err = config.Delete(key)
				if err != nil {
					return err
				}
				fmt.Printf("Deleted %v %v\n", v.Type, v.Name)
			}

			return nil
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&kubeconfig, "kubeconfig", "c", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "",
		"Select a Kubernetes namespace")

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
	rootCmd.AddCommand(injectCmd)
	rootCmd.AddCommand(version.VersionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		glog.Error(err)
		os.Exit(-1)
	}
}

// Set the schema, key, and namespace
// The schema is based on the kind (for example "route-rule" or "destination-policy")
// name represents the name of an instance
func setup(kind, name string) error {
	var singularForm = map[string]string{
		"route-rules":          "route-rule",
		"destination-policies": "destination-policy",
	}
	if singular, ok := singularForm[kind]; ok {
		kind = singular
	}

	// set proto schema
	var ok bool
	schema, ok = model.IstioConfig[kind]
	if !ok {
		return fmt.Errorf("Istio doesn't have configuration type %s, the types are %v",
			kind, strings.Join(model.IstioConfig.Kinds(), ", "))
	}

	// use default namespace by default
	if namespace == "" {
		namespace = api.NamespaceDefault
	}

	// set the config key
	key = model.Key{
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
	}

	return nil
}

// readInputs reads multiple documents from the input and checks with the schema
func readInputs() ([]inputDoc, error) {

	var reader io.Reader
	var err error

	if file == "" {
		reader = os.Stdin
	} else {
		reader, err = os.Open(file)
		if err != nil {
			return nil, err
		}
	}

	var varr []inputDoc

	// We store route-rules as a YaML stream; there may be more than one decoder.
	yamlDecoder := yaml.NewYAMLOrJSONDecoder(reader, 512*1024)
	for {
		v := inputDoc{}
		err = yamlDecoder.Decode(&v)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("cannot parse proto message: %v", err)
		}

		// Do a second decode pass, to get the data into structured format
		byteRule, err := json.Marshal(v.Spec)
		if err != nil {
			return nil, fmt.Errorf("could not encode Spec: %v", err)
		}

		ischema, ok := model.IstioConfig[v.Type]
		if !ok {
			return nil, fmt.Errorf("unknown spec type %s", v.Type)
		}
		rr, err := ischema.FromJSON(string(byteRule))
		if err != nil {
			return nil, fmt.Errorf("cannot parse proto message: %v", err)
		}
		glog.V(2).Infof("Parsed %v %v into %v %v", v.Type, v.Name, ischema.MessageName, rr)

		v.ParsedSpec = rr

		varr = append(varr, v)
	}

	return varr, nil
}

// Print a simple list of names
func printShortOutput(list map[model.Key]proto.Message) error {
	for key := range list {
		fmt.Printf("%v\n", key.Name)
	}

	return nil
}

// Print as YAML
func printYamlOutput(list map[model.Key]proto.Message) error {
	var retVal error

	for key, item := range list {
		out, err := schema.ToYAML(item)
		if err != nil {
			retVal = multierror.Append(retVal, err)
		} else {
			fmt.Printf("kind: %s\n", key.Kind)
			fmt.Printf("name: %s\n", key.Name)
			fmt.Printf("namespace: %s\n", key.Namespace)
			fmt.Println("spec:")
			lines := strings.Split(out, "\n")
			for _, line := range lines {
				if line != "" {
					fmt.Printf("  %s\n", line)
				}
			}
		}
		fmt.Println("---")
	}

	return retVal
}
