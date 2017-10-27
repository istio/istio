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
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pilot/adapter/config/crd"
	"istio.io/istio/pilot/cmd"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform/kube"
	"istio.io/istio/pilot/tools/version"
)

const (
	kubePlatform = "kube"
)

var (
	platform string

	kubeconfig       string
	namespace        string
	istioNamespace   string
	istioContext     string
	istioAPIServer   string
	defaultNamespace string

	// input file name
	file string

	// output format (yaml or short)
	outputFormat string

	rootCmd = &cobra.Command{
		Use:               "istioctl",
		Short:             "Istio control interface",
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		Long: `
Istio configuration command line utility.

Create, list, modify, and delete configuration resources in the Istio
system.

Available routing and traffic management configuration types:

	[routerule ingressrule egressrule destinationpolicy]

See http://istio.io/docs/reference for an overview of routing rules
and destination policies.

`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			defaultNamespace = getDefaultNamespace(kubeconfig)
		},
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
			varr, others, err := readInputs()
			if err != nil {
				return err
			}
			if len(varr) == 0 && len(others) == 0 {
				return errors.New("nothing to create")
			}
			for _, config := range varr {
				if config.Namespace, err = handleNamespaces(config.Namespace); err != nil {
					return err
				}

				var configClient *crd.Client
				if configClient, err = newClient(); err != nil {
					return err
				}
				var rev string
				if rev, err = configClient.Create(config); err != nil {
					return err
				}
				fmt.Printf("Created config %v at revision %v\n", config.Key(), rev)
			}

			if len(others) > 0 {
				if err = preprocMixerConfig(others); err != nil {
					return err
				}
				otherClient, resources, oerr := prepareClientForOthers(others)
				if oerr != nil {
					return oerr
				}
				var errs *multierror.Error
				var updated crd.IstioKind
				for _, config := range others {
					resource, ok := resources[config.Kind]
					if !ok {
						errs = multierror.Append(errs, fmt.Errorf("kind %s is not known", config.Kind))
						continue
					}
					err = otherClient.Post().
						Namespace(config.Namespace).
						Resource(resource.Name).
						Body(&config).
						Do().
						Into(&updated)
					if err != nil {
						errs = multierror.Append(errs, err)
						continue
					}
					key := model.Key(config.Kind, config.Name, config.Namespace)
					fmt.Printf("Created config %s at revision %v\n", key, updated.ResourceVersion)
				}
				if errs != nil {
					return errs
				}
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
			varr, others, err := readInputs()
			if err != nil {
				return err
			}
			if len(varr) == 0 && len(others) == 0 {
				return errors.New("nothing to replace")
			}
			for _, config := range varr {
				if config.Namespace, err = handleNamespaces(config.Namespace); err != nil {
					return err
				}

				var configClient *crd.Client
				if configClient, err = newClient(); err != nil {
					return err
				}
				// fill up revision
				if config.ResourceVersion == "" {
					current, exists := configClient.Get(config.Type, config.Name, config.Namespace)
					if exists {
						config.ResourceVersion = current.ResourceVersion
					}
				}

				var newRev string
				if newRev, err = configClient.Update(config); err != nil {
					return err
				}

				fmt.Printf("Updated config %v to revision %v\n", config.Key(), newRev)
			}

			if len(others) > 0 {
				if err = preprocMixerConfig(others); err != nil {
					return err
				}
				otherClient, resources, oerr := prepareClientForOthers(others)
				if oerr != nil {
					return oerr
				}
				var errs *multierror.Error
				var current crd.IstioKind
				var updated crd.IstioKind
				for _, config := range others {
					resource, ok := resources[config.Kind]
					if !ok {
						errs = multierror.Append(errs, fmt.Errorf("kind %s is not known", config.Kind))
						continue
					}
					if config.ResourceVersion == "" {
						err = otherClient.Get().
							Namespace(config.Namespace).
							Name(config.Name).
							Resource(resource.Name).
							Do().
							Into(&current)
						if err == nil && current.ResourceVersion != "" {
							config.ResourceVersion = current.ResourceVersion
						}
					}

					err = otherClient.Put().
						Namespace(config.Namespace).
						Name(config.Name).
						Resource(resource.Name).
						Body(&config).
						Do().
						Into(&updated)
					if err != nil {
						errs = multierror.Append(errs, err)
						continue
					}
					key := model.Key(config.Kind, config.Name, config.Namespace)
					fmt.Printf("Updated config %s to revision %v\n", key, updated.ResourceVersion)
				}
				if errs != nil {
					return errs
				}
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

			ns, _ := handleNamespaces(v1.NamespaceAll)
			var configs []model.Config
			if len(args) > 1 {
				config, exists := configClient.Get(typ.Type, args[1], ns)
				if exists {
					configs = append(configs, *config)
				}
			} else {
				configs, err = configClient.List(typ.Type, ns)
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
			varr, others, err := readInputs()
			if err != nil {
				return err
			}
			if len(varr) == 0 && len(others) == 0 {
				return errors.New("nothing to delete")
			}
			for _, config := range varr {
				if config.Namespace, err = handleNamespaces(config.Namespace); err != nil {
					return err
				}

				// compute key if necessary
				if err = configClient.Delete(config.Type, config.Name, config.Namespace); err != nil {
					errs = multierror.Append(errs, fmt.Errorf("cannot delete %s: %v", config.Key(), err))
				} else {
					fmt.Printf("Deleted config: %v\n", config.Key())
				}
			}
			if errs != nil {
				return errs
			}

			if len(others) > 0 {
				if err = preprocMixerConfig(others); err != nil {
					return err
				}
				otherClient, resources, oerr := prepareClientForOthers(others)
				if oerr != nil {
					return oerr
				}
				for _, config := range others {
					resource, ok := resources[config.Kind]
					if !ok {
						errs = multierror.Append(errs, fmt.Errorf("kind %s is not known", config.Kind))
						continue
					}
					err = otherClient.Delete().
						Namespace(config.Namespace).
						Resource(resource.Name).
						Do().
						Error()
					if err != nil {
						errs = multierror.Append(errs, fmt.Errorf("failed to delete: %v", err))
						continue
					}
					fmt.Printf("Deleted config: %s\n", model.Key(config.Kind, config.Name, config.Namespace))
				}
			}

			return errs
		},
	}

	configCmd = &cobra.Command{
		Use:   "context-create --api-server http://<ip>:<port>",
		Short: "Create a kubeconfig file suitable for use with istioctl in a non kubernetes environment",
		Example: `
		# Create a config file for the api server.
		istioctl context-create --api-server http://127.0.0.1:8080
		`,
		RunE: func(c *cobra.Command, args []string) error {
			if istioAPIServer == "" {
				c.Println(c.UsageString())
				return fmt.Errorf("specify the the Istio api server IP")
			}

			u, err := url.ParseRequestURI(istioAPIServer)
			if err != nil {
				c.Println(c.UsageString())
				return err
			}

			configAccess := clientcmd.NewDefaultPathOptions()
			// use specified kubeconfig file for the location of the config to create or modify
			configAccess.GlobalFile = kubeconfig

			// gets existing kubeconfig or returns new empty config
			config, err := configAccess.GetStartingConfig()
			if err != nil {
				return err
			}

			cluster, exists := config.Clusters[istioContext]
			if !exists {
				cluster = clientcmdapi.NewCluster()
			}
			cluster.Server = u.String()
			config.Clusters[istioContext] = cluster

			context, exists := config.Contexts[istioContext]
			if !exists {
				context = clientcmdapi.NewContext()
			}
			context.Cluster = istioContext
			config.Contexts[istioContext] = context

			contextSwitched := false
			if config.CurrentContext != "" && config.CurrentContext != istioContext {
				contextSwitched = true
			}
			config.CurrentContext = istioContext
			if err = clientcmd.ModifyConfig(configAccess, *config, false); err != nil {
				return err
			}

			if contextSwitched {
				fmt.Printf("kubeconfig context switched to %q\n", istioContext)
			}
			fmt.Println("Context created")
			return nil
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

	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", v1.NamespaceAll,
		"Config namespace")

	defaultContext := "istio"
	configCmd.PersistentFlags().StringVar(&istioContext, "context", defaultContext,
		"Kubernetes configuration file context name")
	configCmd.PersistentFlags().StringVar(&istioAPIServer, "api-server", "",
		"URL for Istio api server")

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
	rootCmd.AddCommand(configCmd)
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
func readInputs() ([]model.Config, []crd.IstioKind, error) {
	var reader io.Reader
	switch file {
	case "":
		return nil, nil, errors.New("filename not specified (see --filename or -f)")
	case "-":
		reader = os.Stdin
	default:
		var err error
		if reader, err = os.Open(file); err != nil {
			return nil, nil, err
		}
	}
	input, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, nil, err
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

func preprocMixerConfig(configs []crd.IstioKind) error {
	var err error
	for i, config := range configs {
		if configs[i].Namespace, err = handleNamespaces(config.Namespace); err != nil {
			return err
		}
		if config.APIVersion == "" {
			configs[i].APIVersion = crd.IstioAPIGroupVersion.String()
		}
		// TODO: invokes the mixer validation webhook.
	}
	return nil
}

func apiResources(config *rest.Config, configs []crd.IstioKind) (map[string]metav1.APIResource, error) {
	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}
	resources, err := client.ServerResourcesForGroupVersion(crd.IstioAPIGroupVersion.String())
	if err != nil {
		return nil, err
	}
	kindsSet := map[string]bool{}
	for _, config := range configs {
		if !kindsSet[config.Kind] {
			kindsSet[config.Kind] = true
		}
	}
	result := make(map[string]metav1.APIResource, len(kindsSet))
	for _, resource := range resources.APIResources {
		if kindsSet[resource.Kind] {
			result[resource.Kind] = resource
		}
	}
	return result, nil
}

func restClientForOthers(config *rest.Config) (*rest.RESTClient, error) {
	configCopied := *config
	configCopied.ContentConfig = dynamic.ContentConfig()
	configCopied.GroupVersion = &crd.IstioAPIGroupVersion
	return rest.RESTClientFor(config)
}

func prepareClientForOthers(configs []crd.IstioKind) (*rest.RESTClient, map[string]metav1.APIResource, error) {
	restConfig, err := crd.CreateRESTConfig(kubeconfig)
	if err != nil {
		return nil, nil, err
	}
	resources, err := apiResources(restConfig, configs)
	if err != nil {
		return nil, nil, err
	}
	client, err := restClientForOthers(restConfig)
	if err != nil {
		return nil, nil, err
	}
	return client, resources, nil
}

func getDefaultNamespace(kubeconfig string) string {
	configAccess := clientcmd.NewDefaultPathOptions()
	// use specified kubeconfig file for the location of the config to read
	configAccess.GlobalFile = kubeconfig

	// gets existing kubeconfig or returns new empty config
	config, err := configAccess.GetStartingConfig()
	if err != nil {
		return v1.NamespaceDefault
	}

	namespace := config.Contexts[config.CurrentContext].Namespace
	if namespace == "" {
		return v1.NamespaceDefault
	}
	return namespace
}

func handleNamespaces(objectNamespace string) (string, error) {
	if objectNamespace != "" && namespace != "" && namespace != objectNamespace {
		return "", fmt.Errorf(`the namespace from the provided object "%s" does `+
			`not match the namespace "%s". You must pass '--namespace=%s' to perform `+
			`this operation`, objectNamespace, namespace, objectNamespace)
	}

	if namespace != "" {
		return namespace, nil
	}

	if objectNamespace != "" {
		return objectNamespace, nil
	}
	return defaultNamespace, nil
}
