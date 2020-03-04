// Copyright 2019 Istio Authors
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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"

	"istio.io/pkg/log"

	"istio.io/istio/istioctl/pkg/authz"
	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pilot/pkg/security/authz/converter"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
)

var (
	printAll                 bool
	configDumpFile           string
	v1Files                  []string
	serviceFiles             []string
	rootNamespace            string
	allowNoClusterRbacConfig bool
)

var (
	checkCmd = &cobra.Command{
		Use:   "check <pod-name>[.<pod-namespace>]",
		Short: "Check Envoy config dump for authorization configuration.",
		Long: `Check reads the Envoy config dump and checks the filter configuration
related to authorization. For example, it shows whether or not the Envoy is configured
with authorization and the rules used in the authorization.

The Envoy config dump could be provided either by pod name or from a config dump file
(the whole output of http://localhost:15000/config_dump of an Envoy instance).

THIS COMMAND IS STILL UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.
`,
		Example: `  # Check Envoy authorization configuration for pod httpbin-88ddbcfdd-nt5jb:
  istioctl x authz check httpbin-88ddbcfdd-nt5jb

  # Check Envoy authorization configuration from a config dump file:
  istioctl x authz check -f httpbin_config_dump.json`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("check requires only pod name")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var configDump *configdump.Wrapper
			var err error
			if configDumpFile != "" {
				configDump, err = getConfigDumpFromFile(configDumpFile)
				if err != nil {
					return fmt.Errorf("failed to get config dump from file %s: %s", configDumpFile, err)
				}
			} else if len(args) == 1 {
				podName, podNamespace := handlers.InferPodInfo(args[0], handlers.HandleNamespace(namespace, defaultNamespace))
				configDump, err = getConfigDumpFromPod(podName, podNamespace)
				if err != nil {
					return fmt.Errorf("failed to get config dump from pod %s", args[0])
				}
			} else {
				return fmt.Errorf("expecting pod name or config dump, found: %d", len(args))
			}

			analyzer, err := authz.NewAnalyzer(configDump)
			if err != nil {
				return err
			}
			analyzer.Print(cmd.OutOrStdout(), printAll)
			return nil
		},
	}

	convertCmd = &cobra.Command{
		Use:   "convert",
		Short: "Convert v1alpha1 RBAC policy to v1beta1 authorization policy",
		Long: `Convert Istio v1alpha1 RBAC policy to v1beta1 authorization policy. By default,
The command talks to Istio Pilot and Kubernetes API server to get all the information
needed for the conversion, including the v1alpha1 RBAC policies in the current cluster,
the value of the root namespace and the Kubernetes services that provide the mapping from the
service name to workload selector.

The tool can also be used in an offline mode when specified with flag -f. In this mode,
the tool doesn't access the network and all needed information is provided
through the command line.

Note: The converter tool makes a best effort attempt to keep the syntax unchanged during
the conversion. However, in some cases, strict mapping with equivalent syntax is not
possible (e.g., constraints no longer supported in the new workload oriented model).

PLEASE ALWAYS REVIEW THE CONVERTED POLICIES BEFORE APPLYING.
`,
		Example: `  # Convert the v1alpha1 RBAC policy in the current cluster:
  istioctl x authz convert > authorization-policies.yaml

  # Convert the v1alpha1 RBAC policy in the given file: 
  istioctl x authz convert -f v1alpha1-policy-1.yaml,v1alpha1-policy-2.yaml
  -s my-services.yaml -r my-root-namespace > authorization-policies.yaml
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			var authorizationPolicies *model.AuthorizationPolicies

			if len(v1Files) != 0 {
				if len(serviceFiles) == 0 {
					return fmt.Errorf("service must be provided when -f is used")
				}
				if rootNamespace == "" {
					return fmt.Errorf("root namespace must not be empty when -f is used")
				}

				authorizationPolicies, err = createAuthorizationPoliciesFromFiles(v1Files, rootNamespace)
				if err != nil {
					return fmt.Errorf("failed to create the AuthorizationPolicies: %v", err)
				}
			} else {
				authorizationPolicies, err = getAuthorizationPoliciesFromCluster()
				if err != nil {
					return fmt.Errorf("failed to get the v1alpha1 RBAC policies: %v", err)
				}
				if rootNamespace != "" && authorizationPolicies.RootNamespace != rootNamespace {
					log.Warnf("override root namespace from %q to %q", authorizationPolicies.RootNamespace, rootNamespace)
					authorizationPolicies.RootNamespace = rootNamespace
				}
			}

			var namespaceToServiceToSelector map[string]converter.ServiceToWorkloadLabels
			namespaceToServiceToSelector, err = getNamespaceToServiceToSelector(serviceFiles, authorizationPolicies.ListV1alpha1Namespaces())
			if err != nil {
				return fmt.Errorf("failed to get the k8s service definitions: %v", err)
			}

			out, err := converter.Convert(authorizationPolicies, namespaceToServiceToSelector, allowNoClusterRbacConfig)
			if err != nil {
				return err
			}

			writer := cmd.OutOrStdout()
			_, err = writer.Write([]byte(out))
			if err != nil {
				return fmt.Errorf("failed to write to stdout: %v", err)
			}
			return nil
		},
		PersistentPreRunE: func(c *cobra.Command, args []string) error {
			// The istioctl x auth convert command is typically redirected to a .yaml file;
			// Redirect log messages to stderr, not stdout
			_ = c.Root().PersistentFlags().Set("log_target", "stderr")
			return c.Root().PersistentPreRunE(c, args)
		},
	}
)

func getConfigDumpFromFile(filename string) (*configdump.Wrapper, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Errorf("failed to close %s: %s", filename, err)
		}
	}()
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}

	envoyConfig := &configdump.Wrapper{}
	if err := envoyConfig.UnmarshalJSON(data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal proxy config: %s", err)
	}
	return envoyConfig, nil
}

func getConfigDumpFromPod(podName, podNamespace string) (*configdump.Wrapper, error) {
	kubeClient, err := kubernetes.NewClient(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	pods, err := kubeClient.GetIstioPods(podNamespace, map[string]string{
		"fieldSelector": "metadata.name=" + podName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %s", err)
	}
	if len(pods) != 1 {
		return nil, fmt.Errorf("expecting only 1 pod for %s.%s, found: %d", podName, podNamespace, len(pods))
	}

	data, err := kubeClient.EnvoyDo(podName, podNamespace, "GET", "config_dump", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get proxy config for %s.%s: %s", podName, podNamespace, err)
	}
	envoyConfig := &configdump.Wrapper{}
	if err := envoyConfig.UnmarshalJSON(data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal proxy config: %s", err)
	}
	return envoyConfig, nil
}

func createAuthorizationPoliciesFromFiles(files []string, rootNamespace string) (*model.AuthorizationPolicies, error) {
	var authorizationPolicies *model.AuthorizationPolicies
	var configs []model.Config
	for _, file := range files {
		rbacFileBuf, err := ioutil.ReadFile(file)
		if err != nil {
			return nil, err
		}
		configFromFile, _, err := crd.ParseInputs(string(rbacFileBuf))
		if err != nil {
			return nil, err
		}
		configs = append(configs, configFromFile...)
	}
	store := model.MakeIstioStore(memory.Make(collections.Pilot))
	for _, config := range configs {
		if _, err := store.Create(config); err != nil {
			return nil, err
		}
	}
	env := &model.Environment{
		IstioConfigStore: store,
	}
	var err error
	authorizationPolicies, err = model.GetAuthorizationPolicies(env)
	if err != nil {
		return nil, err
	}
	authorizationPolicies.RootNamespace = rootNamespace
	return authorizationPolicies, nil
}

func getAuthorizationPoliciesFromCluster() (*model.AuthorizationPolicies, error) {
	var authorizationPolicies *model.AuthorizationPolicies
	kubeClient, err := clientExecFactory(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	results, err := kubeClient.AllPilotsDiscoveryDo(istioNamespace, "GET", "/debug/authorizationz", nil)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("received empty response for authorization information")
	}
	for _, result := range results {
		authzDebug := v2.AuthorizationDebug{}
		if err := json.Unmarshal(result, &authzDebug); err != nil {
			log.Debugf("JSON unmarshal failed: %v", err)
			continue
		}
		authorizationPolicies = authzDebug.AuthorizationPolicies
		// Break once we found a successful response from Pilot.
		break
	}
	if authorizationPolicies == nil {
		return nil, fmt.Errorf("no v1alpha1 RBAC policy")
	}
	return authorizationPolicies, nil
}

func getNamespaceToServiceToSelector(files, namespaces []string) (map[string]converter.ServiceToWorkloadLabels, error) {
	k8sClient, err := kube.CreateClientset("", "")
	if err != nil {
		return nil, fmt.Errorf("failed to create the Kubernetes client: %v", err)
	}
	var services []v1.Service
	if len(files) != 0 {
		for _, filename := range files {
			fileBuf, err := ioutil.ReadFile(filename)
			if err != nil {
				return nil, err
			}
			reader := bytes.NewReader(fileBuf)
			yamlDecoder := kubeyaml.NewYAMLOrJSONDecoder(reader, 512*1024)
			for {
				svc := v1.Service{}
				err = yamlDecoder.Decode(&svc)
				if err == io.EOF {
					break
				}
				if err != nil {
					return nil, err
				}
				services = append(services, svc)
			}
		}
	} else {
		for _, ns := range namespaces {
			rets, err := k8sClient.CoreV1().Services(ns).List(metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			services = append(services, rets.Items...)
		}
	}

	namespaceToServiceToSelector := make(map[string]converter.ServiceToWorkloadLabels)
	for _, svc := range services {
		if len(svc.Spec.Selector) == 0 {
			log.Warnf("ignored service with empty selector: %s.%s", svc.Name, svc.Namespace)
			continue
		}
		if _, found := namespaceToServiceToSelector[svc.Namespace]; !found {
			namespaceToServiceToSelector[svc.Namespace] = make(converter.ServiceToWorkloadLabels)
		}
		namespaceToServiceToSelector[svc.Namespace][svc.Name] = svc.Spec.Selector
	}

	return namespaceToServiceToSelector, nil
}

// AuthZ groups commands used for inspecting and interacting the authorization policy.
// Note: this is still under active development and is not ready for real use.
func AuthZ() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "authz",
		Short: "Inspect and interact with authorization policies",
		Long: `Commands to inspect and interact with the authorization policies
  check - check Envoy config dump for authorization configuration
  convert - convert v1alpha1 RBAC policies to v1beta1 authorization policies
`,
		Example: `  # Check Envoy authorization configuration for pod httpbin-88ddbcfdd-nt5jb:
  istioctl x authz check httpbin-88ddbcfdd-nt5jb

  # Convert the v1alpha1 RBAC policies in the current cluster:
  istioctl x authz convert > authorization-policies.yaml

  # Convert the v1alpha1 RBAC policies in the file with the given services and root namespace:
  istioctl x authz convert -f rbac-policies.yaml -s my-service.yaml -r istio-system > authorization-policies.yaml
`,
	}

	cmd.AddCommand(checkCmd)
	cmd.AddCommand(convertCmd)
	return cmd
}

func init() {
	checkCmd.PersistentFlags().BoolVarP(&printAll, "all", "a", false,
		"Show additional information (e.g. SNI and ALPN)")
	checkCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"The json file with Envoy config dump to be checked")
	convertCmd.PersistentFlags().StringSliceVarP(&v1Files, "file", "f", []string{},
		"The yaml file with v1alpha1 RBAC policies to be converted")
	convertCmd.PersistentFlags().StringSliceVarP(&serviceFiles, "service", "s", []string{},
		"The yaml file with Kubernetes services for the mapping from the service name to workload selector, used with -f")
	convertCmd.PersistentFlags().StringVarP(&rootNamespace, "rootNamespace", "r", "istio-system",
		"Override the root namespace used in the conversion")
	convertCmd.PersistentFlags().BoolVarP(&allowNoClusterRbacConfig, "allowNoClusterRbacConfig", "", false,
		"Continue the conversion even if there is no ClusterRbacConfig in the cluster")
}
