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
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ghodss/yaml"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"

	// import all known client auth plugins
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/istioctl/cmd/istioctl/gendeployment"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/collateral"
	kubecfg "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
)

const (
	kubePlatform = "kube"

	// Headings for short format listing of unknown types
	unknownShortOutputHeading = "NAME\tKIND\tNAMESPACE\tAGE"
)

var (
	platform string

	kubeconfig       string
	configContext    string
	namespace        string
	istioNamespace   string
	istioContext     string
	istioAPIServer   string
	defaultNamespace string

	// input file name
	file string

	// output format (yaml or short)
	outputFormat     string
	getAllNamespaces bool

	// Create a model.ConfigStore (or sortedConfigStore)
	clientFactory = newClient

	// Create a kubernetes.ExecClient (or mockExecClient)
	clientExecFactory = newExecClient

	loggingOptions = log.DefaultOptions()

	// sortWeight defines the output order for "get all".  We show the V3 types first.
	sortWeight = map[string]int{
		model.Gateway.Type:         10,
		model.VirtualService.Type:  5,
		model.DestinationRule.Type: 3,
		model.ServiceEntry.Type:    1,
	}

	// mustList tracks which Istio types we SHOULD NOT silently ignore if we can't list.
	// The user wants reasonable error messages when doing `get all` against a different
	// server version.
	mustList = map[string]bool{
		model.Gateway.Type:              true,
		model.VirtualService.Type:       true,
		model.DestinationRule.Type:      true,
		model.ServiceEntry.Type:         true,
		model.HTTPAPISpec.Type:          true,
		model.HTTPAPISpecBinding.Type:   true,
		model.QuotaSpec.Type:            true,
		model.QuotaSpecBinding.Type:     true,
		model.AuthenticationPolicy.Type: true,
		model.ServiceRole.Type:          true,
		model.ServiceRoleBinding.Type:   true,
		model.RbacConfig.Type:           true,
	}

	// Headings for short format listing specific to type
	shortOutputHeadings = map[string]string{
		"gateway":          "GATEWAY NAME\tHOSTS\tNAMESPACE\tAGE",
		"virtual-service":  "VIRTUAL-SERVICE NAME\tGATEWAYS\tHOSTS\t#HTTP\t#TCP\tNAMESPACE\tAGE",
		"destination-rule": "DESTINATION-RULE NAME\tHOST\tSUBSETS\tNAMESPACE\tAGE",
		"service-entry":    "SERVICE-ENTRY NAME\tHOSTS\tPORTS\tNAMESPACE\tAGE",
	}

	// Formatters for short format listing specific to type
	shortOutputters = map[string]func(model.Config, io.Writer){
		"gateway":          printShortGateway,
		"virtual-service":  printShortVirtualService,
		"destination-rule": printShortDestinationRule,
		"service-entry":    printShortServiceEntry,
	}

	// all resources will be migrated out of config.istio.io to their own api group mapping to package path.
	// TODO(xiaolanz) legacy group exists until we find out a client for mixer
	legacyIstioAPIGroupVersion = schema.GroupVersion{
		Group:   "config.istio.io",
		Version: "v1alpha2",
	}

	rootCmd = &cobra.Command{
		Use:               "istioctl",
		Short:             "Istio control interface.",
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		Long: `Istio configuration command line utility for service operators to
debug and diagnose their Istio mesh.
`,
		PersistentPreRunE: istioPersistentPreRunE,
	}

	postCmd = &cobra.Command{
		Use:        "create",
		Deprecated: "Use `kubectl create` instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl)",
		Short:      "Create policies and rules",
		Example:    "istioctl create -f example-routing.yaml",
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

				var configClient model.ConfigStore
				if configClient, err = clientFactory(); err != nil {
					return err
				}
				var rev string
				if rev, err = configClient.Create(config); err != nil {
					return err
				}
				c.Printf("Created config %v at revision %v\n", config.Key(), rev)
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
		Use:        "replace",
		Deprecated: "Use `kubectl apply` instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl)",
		Short:      "Replace existing policies and rules",
		Example:    "istioctl replace -f example-routing.yaml",
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

				var configClient model.ConfigStore
				if configClient, err = clientFactory(); err != nil {
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
		Use:        "get <type> [<name>]",
		Deprecated: "Use `kubectl get` instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl)",
		Short:      "Retrieve policies and rules",
		Example: `# List all virtual services
istioctl get virtualservices

# List all destination rules
istioctl get destinationrules

# Get a specific virtual service named bookinfo
istioctl get virtualservice bookinfo
`,
		RunE: func(c *cobra.Command, args []string) error {
			configClient, err := clientFactory()
			if err != nil {
				return err
			}
			if len(args) < 1 {
				c.Println(c.UsageString())
				return fmt.Errorf("specify the type of resource to get. Types are %v",
					strings.Join(supportedTypes(configClient), ", "))
			}

			getByName := len(args) > 1
			if getAllNamespaces && getByName {
				return errors.New("a resource cannot be retrieved by name across all namespaces")
			}

			var typs []model.ProtoSchema
			if !getByName && strings.ToLower(args[0]) == "all" {
				typs = configClient.ConfigDescriptor()
			} else {
				typ, err := protoSchema(configClient, args[0])
				if err != nil {
					c.Println(c.UsageString())
					return err
				}
				typs = []model.ProtoSchema{typ}
			}

			var ns string
			if getAllNamespaces {
				ns = v1.NamespaceAll
			} else {
				ns, _ = handleNamespaces(namespace)
			}

			var errs error
			var configs []model.Config
			if getByName {
				config, exists := configClient.Get(typs[0].Type, args[1], ns)
				if exists {
					configs = append(configs, *config)
				}
			} else {
				for _, typ := range typs {
					typeConfigs, err := configClient.List(typ.Type, ns)
					if err == nil {
						configs = append(configs, typeConfigs...)
					} else {
						if mustList[typ.Type] {
							errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("Can't list %v:", typ.Type)))
						}
					}
				}
			}

			if len(configs) == 0 {
				c.Println("No resources found.")
				return errs
			}

			var outputters = map[string](func(io.Writer, model.ConfigStore, []model.Config)){
				"yaml":  printYamlOutput,
				"short": printShortOutput,
			}

			if outputFunc, ok := outputters[outputFormat]; ok {
				outputFunc(c.OutOrStdout(), configClient, configs)
			} else {
				return fmt.Errorf("unknown output format %v. Types are yaml|short", outputFormat)
			}

			return errs
		},

		ValidArgs:  configTypeResourceNames(model.IstioConfigTypes),
		ArgAliases: configTypePluralResourceNames(model.IstioConfigTypes),
	}

	deleteCmd = &cobra.Command{
		Use:        "delete <type> <name> [<name2> ... <nameN>]",
		Deprecated: "Use `kubectl delete` instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl)",
		Short:      "Delete policies or rules",
		Example: `# Delete a rule using the definition in example-routing.yaml.
istioctl delete -f example-routing.yaml

# Delete the virtual service bookinfo
istioctl delete virtualservice bookinfo
`,
		RunE: func(c *cobra.Command, args []string) error {
			configClient, errs := clientFactory()
			if errs != nil {
				return errs
			}
			// If we did not receive a file option, get names of resources to delete from command line
			if file == "" {
				if len(args) < 2 {
					c.Println(c.UsageString())
					return fmt.Errorf("provide configuration type and name or -f option")
				}
				typ, err := protoSchema(configClient, args[0])
				if err != nil {
					return err
				}
				ns, err := handleNamespaces(namespace)
				if err != nil {
					return err
				}
				for i := 1; i < len(args); i++ {
					if err := configClient.Delete(typ.Type, args[i], ns); err != nil {
						errs = multierror.Append(errs,
							fmt.Errorf("cannot delete %s: %v", args[i], err))
					} else {
						c.Printf("Deleted config: %v %v\n", args[0], args[i])
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
					c.Printf("Deleted config: %v\n", config.Key())
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
						Name(config.Name).
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

		ValidArgs:  configTypeResourceNames(model.IstioConfigTypes),
		ArgAliases: configTypePluralResourceNames(model.IstioConfigTypes),
	}

	contextCmd = &cobra.Command{
		Use: "context-create --api-server http://<ip>:<port>",
		Deprecated: `Use kubectl instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl), e.g.

	$ kubectl config set-context istio --cluster=istio
	$ kubectl config set-cluster istio --server=http://localhost:8080
	$ kubectl config use-context istio
`,
		Short: "Create a kubeconfig file suitable for use with istioctl in a non kubernetes environment",
		Example: `# Create a config file for the api server.
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

	experimentalCmd = &cobra.Command{
		Use:   "experimental",
		Short: "Experimental commands that may be modified or deprecated",
	}
)

func istioPersistentPreRunE(c *cobra.Command, args []string) error {
	if err := log.Configure(loggingOptions); err != nil {
		return err
	}
	defaultNamespace = getDefaultNamespace(kubeconfig)
	return nil
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&platform, "platform", "p", kubePlatform,
		"Istio host platform")

	rootCmd.PersistentFlags().StringVarP(&kubeconfig, "kubeconfig", "c", "",
		"Kubernetes configuration file")

	rootCmd.PersistentFlags().StringVar(&configContext, "context", "",
		"The name of the kubeconfig context to use")

	rootCmd.PersistentFlags().StringVarP(&istioNamespace, "istioNamespace", "i", kube.IstioNamespace,
		"Istio system namespace")

	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", v1.NamespaceAll,
		"Config namespace")

	defaultContext := "istio"
	contextCmd.PersistentFlags().StringVar(&istioContext, "context", defaultContext,
		"Kubernetes configuration file context name")
	contextCmd.PersistentFlags().StringVar(&istioAPIServer, "api-server", "",
		"URL for Istio api server")

	postCmd.PersistentFlags().StringVarP(&file, "file", "f", "",
		"Input file with the content of the configuration objects (if not set, command reads from the standard input)")
	putCmd.PersistentFlags().AddFlag(postCmd.PersistentFlags().Lookup("file"))
	deleteCmd.PersistentFlags().AddFlag(postCmd.PersistentFlags().Lookup("file"))

	getCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "short",
		"Output format. One of:yaml|short")
	getCmd.PersistentFlags().BoolVar(&getAllNamespaces, "all-namespaces", false,
		"If present, list the requested object(s) across all namespaces. Namespace in current "+
			"context is ignored even if specified with --namespace.")

	experimentalCmd.AddCommand(Rbac())

	// Attach the Istio logging options to the command.
	loggingOptions.AttachCobraFlags(rootCmd)

	cmd.AddFlags(rootCmd)

	rootCmd.AddCommand(postCmd)
	rootCmd.AddCommand(putCmd)
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(contextCmd)
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(gendeployment.Command(&istioNamespace))
	rootCmd.AddCommand(experimentalCmd)

	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Control",
		Section: "istioctl CLI",
		Manual:  "Istio Control",
	}))
}

func main() {
	if platform != kubePlatform {
		log.Warnf("Platform '%s' not supported.", platform)
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}

// The protoSchema is based on the kind (for example "virtualservice" or "destinationrule")
func protoSchema(configClient model.ConfigStore, typ string) (model.ProtoSchema, error) {
	for _, desc := range configClient.ConfigDescriptor() {
		switch strings.ToLower(typ) {
		case crd.ResourceName(desc.Type), crd.ResourceName(desc.Plural):
			return desc, nil
		case desc.Type, desc.Plural: // legacy hyphenated resources names
			return model.ProtoSchema{}, fmt.Errorf("%q not recognized. Please use non-hyphenated resource name %q",
				typ, crd.ResourceName(typ))
		}
	}
	return model.ProtoSchema{}, fmt.Errorf("configuration type %s not found, the types are %v",
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
		var in *os.File
		if in, err = os.Open(file); err != nil {
			return nil, nil, err
		}
		defer func() {
			if err = in.Close(); err != nil {
				log.Errorf("Error: close file from %s, %s", file, err)
			}
		}()
		reader = in
	}
	input, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, nil, err
	}
	return crd.ParseInputsWithoutValidation(string(input))
}

// Print a simple list of names
func printShortOutput(writer io.Writer, _ model.ConfigStore, configList []model.Config) {
	// Sort configList by Type
	sort.Slice(configList, func(i, j int) bool { return sortWeight[configList[i].Type] < sortWeight[configList[j].Type] })

	var w tabwriter.Writer
	w.Init(writer, 10, 4, 3, ' ', 0)
	prevType := ""
	var outputter func(model.Config, io.Writer)
	for _, c := range configList {
		if prevType != c.Type {
			if prevType != "" {
				// Place a newline between types when doing 'get all'
				fmt.Fprintf(&w, "\n")
			}
			heading, ok := shortOutputHeadings[c.Type]
			if !ok {
				heading = unknownShortOutputHeading
			}
			fmt.Fprintf(&w, "%s\n", heading)
			prevType = c.Type

			if outputter, ok = shortOutputters[c.Type]; !ok {
				outputter = printShortConfig
			}
		}

		outputter(c, &w)
	}
	w.Flush() // nolint: errcheck
}

func kindAsString(config model.Config) string {
	return fmt.Sprintf("%s.%s.%s",
		crd.KabobCaseToCamelCase(config.Type),
		config.Group,
		config.Version,
	)
}

func printShortConfig(config model.Config, w io.Writer) {
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
		config.Name,
		kindAsString(config),
		config.Namespace,
		renderTimestamp(config.CreationTimestamp))
}

func printShortVirtualService(config model.Config, w io.Writer) {
	virtualService, ok := config.Spec.(*v1alpha3.VirtualService)
	if !ok {
		fmt.Fprintf(w, "Not a virtualservice: %v", config)
		return
	}

	fmt.Fprintf(w, "%s\t%s\t%s\t%5d\t%4d\t%s\t%s\n",
		config.Name,
		strings.Join(virtualService.Gateways, ","),
		strings.Join(virtualService.Hosts, ","),
		len(virtualService.Http),
		len(virtualService.Tcp),
		config.Namespace,
		renderTimestamp(config.CreationTimestamp))
}

func printShortDestinationRule(config model.Config, w io.Writer) {
	destinationRule, ok := config.Spec.(*v1alpha3.DestinationRule)
	if !ok {
		fmt.Fprintf(w, "Not a destinationrule: %v", config)
		return
	}

	subsets := make([]string, 0)
	for _, subset := range destinationRule.Subsets {
		subsets = append(subsets, subset.Name)
	}

	fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
		config.Name,
		destinationRule.Host,
		strings.Join(subsets, ","),
		config.Namespace,
		renderTimestamp(config.CreationTimestamp))
}

func printShortServiceEntry(config model.Config, w io.Writer) {
	serviceEntry, ok := config.Spec.(*v1alpha3.ServiceEntry)
	if !ok {
		fmt.Fprintf(w, "Not a serviceentry: %v", config)
		return
	}

	ports := make([]string, 0)
	for _, port := range serviceEntry.Ports {
		ports = append(ports, fmt.Sprintf("%s/%d", port.Protocol, port.Number))
	}

	fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
		config.Name,
		strings.Join(serviceEntry.Hosts, ","),
		strings.Join(ports, ","),
		config.Namespace,
		renderTimestamp(config.CreationTimestamp))
}

func printShortGateway(config model.Config, w io.Writer) {
	gateway, ok := config.Spec.(*v1alpha3.Gateway)
	if !ok {
		fmt.Fprintf(w, "Not a gateway: %v", config)
		return
	}

	// Determine the servers
	servers := make(map[string]bool)
	for _, server := range gateway.Servers {
		for _, host := range server.Hosts {
			servers[host] = true
		}
	}
	hosts := make([]string, 0)
	for host := range servers {
		hosts = append(hosts, host)
	}

	fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
		config.Name, strings.Join(hosts, ","), config.Namespace,
		renderTimestamp(config.CreationTimestamp))
}

// Print as YAML
func printYamlOutput(writer io.Writer, configClient model.ConfigStore, configList []model.Config) {
	descriptor := configClient.ConfigDescriptor()
	for _, config := range configList {
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
		fmt.Fprint(writer, string(bytes))
		fmt.Fprintln(writer, "---")
	}
}

func newClient() (model.ConfigStore, error) {
	return crd.NewClient(kubeconfig, configContext, model.IstioConfigTypes, "")
}

func supportedTypes(configClient model.ConfigStore) []string {
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
			configs[i].APIVersion = legacyIstioAPIGroupVersion.String()
		}
		// TODO: invokes the mixer validation webhook.
	}
	return nil
}

func restConfig() (config *rest.Config, err error) {
	config, err = kubecfg.BuildClientConfig(kubeconfig, configContext)

	if err != nil {
		return
	}

	config.GroupVersion = &legacyIstioAPIGroupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON

	types := runtime.NewScheme()
	schemeBuilder := runtime.NewSchemeBuilder(
		func(scheme *runtime.Scheme) error {
			metav1.AddToGroupVersion(scheme, legacyIstioAPIGroupVersion)
			return nil
		})
	err = schemeBuilder.AddToScheme(types)
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(types)}
	return
}

func apiResources(config *rest.Config, configs []crd.IstioKind) (map[string]metav1.APIResource, error) {
	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}
	resources, err := client.ServerResourcesForGroupVersion(legacyIstioAPIGroupVersion.String())
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
	configCopied.GroupVersion = &legacyIstioAPIGroupVersion
	return rest.RESTClientFor(config)
}

func prepareClientForOthers(configs []crd.IstioKind) (*rest.RESTClient, map[string]metav1.APIResource, error) {
	restConfig, err := restConfig()
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

	if kubeconfig != "" {
		// use specified kubeconfig file for the location of the
		// config to read
		configAccess.GlobalFile = kubeconfig
	}

	// gets existing kubeconfig or returns new empty config
	config, err := configAccess.GetStartingConfig()
	if err != nil {
		return v1.NamespaceDefault
	}

	context, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return v1.NamespaceDefault
	}
	if context.Namespace == "" {
		return v1.NamespaceDefault
	}
	return context.Namespace
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

func configTypeResourceNames(configTypes model.ConfigDescriptor) []string {
	resourceNames := make([]string, len(configTypes))
	for _, typ := range configTypes {
		resourceNames = append(resourceNames, crd.ResourceName(typ.Type))
	}
	return resourceNames
}

func configTypePluralResourceNames(configTypes model.ConfigDescriptor) []string {
	resourceNames := make([]string, len(configTypes))
	for _, typ := range configTypes {
		resourceNames = append(resourceNames, crd.ResourceName(typ.Plural))
	}
	return resourceNames
}

// renderTimestamp creates a human-readable age similar to docker and kubectl CLI output
func renderTimestamp(ts time.Time) string {
	if ts.IsZero() {
		return "<unknown>"
	}

	seconds := int(time.Since(ts).Seconds())
	if seconds < -2 {
		return fmt.Sprintf("<invalid>")
	} else if seconds < 0 {
		return fmt.Sprintf("0s")
	} else if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}

	minutes := int(time.Since(ts).Minutes())
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}

	hours := int(time.Since(ts).Hours())
	if hours < 24 {
		return fmt.Sprintf("%dh", hours)
	} else if hours < 365*24 {
		return fmt.Sprintf("%dd", hours/24)
	}
	return fmt.Sprintf("%dy", int((hours/24)/365))
}
