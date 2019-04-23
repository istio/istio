// Copyright 2018 Istio Authors
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

// DEPRECATED - These commands are deprecated and will be removed in future releases.

package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"strings"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	kubecfg "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
)

var (
	istioContext   string
	istioAPIServer string

	// Create a model.ConfigStore (or sortedConfigStore)
	clientFactory = newClient

	// all resources will be migrated out of config.istio.io to their own api group mapping to package path.
	// TODO(xiaolanz) legacy group exists until we find out a client for mixer
	legacyIstioAPIGroupVersion = schema.GroupVersion{
		Group:   "config.istio.io",
		Version: "v1alpha2",
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
					current := configClient.Get(config.Type, config.Name, config.Namespace)
					if current != nil {
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
		Short: "Create a kubeconfig file suitable for use with istioctl in a non-Kubernetes environment",
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
)

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

func init() {
	defaultContext := "istio"
	contextCmd.PersistentFlags().StringVar(&istioContext, "context", defaultContext,
		"Kubernetes configuration file context name")
	contextCmd.PersistentFlags().StringVar(&istioAPIServer, "api-server", "",
		"URL for Istio api server")

	postCmd.PersistentFlags().StringVarP(&file, "file", "f", "",
		"Input file with the content of the configuration objects (if not set, command reads from the standard input)")
	putCmd.PersistentFlags().AddFlag(postCmd.PersistentFlags().Lookup("file"))
	deleteCmd.PersistentFlags().AddFlag(postCmd.PersistentFlags().Lookup("file"))

}
