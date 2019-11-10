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
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"

	kubernetes2 "k8s.io/client-go/kubernetes"

	"istio.io/istio/istioctl/pkg/authz"
	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pilot/pkg/security/authz/converter"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

var (
	printAll               bool
	configDumpFile         string
	policyFiles            []string
	serviceFiles           []string
	meshConfig             string
	istioMeshConfigMapName string
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
		Long: `Convert Istio v1alpha1 RBAC policy to v1beta1 authorization policy. The command talks to Kubernetes
API server to get all the information needed to complete the conversion, including the v1alpha1 RBAC policies in the current
cluster, the Istio config-map for root namespace configuration and the k8s Service translating the
service name to workload selector.

The tool can also be used in offline mode without talking to the Kubernetes API server. In this mode,
all needed information is provided through the command line.

Note: The converter tool makes a best effort attempt to keep the syntax unchanged when
converting v1alph1 RBAC policy to v1beta1 policy. However, in some cases, strict
mapping with equivalent syntax is not possible (e.g., constraints no longer valid
in the new workload oriented model, converting a service name containing a wildcard
to workload selector).

Please always review the converted policies before applying them.

THIS COMMAND IS STILL UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.
`,
		Example: `  # Convert the v1alpha1 RBAC policy in the current cluster:
  istioctl x authz convert > v1beta1-authz.yaml

  # Convert the v1alpha1 RBAC policy provided through command line: 
  istioctl x authz convert -f v1alpha1-policy-1.yaml,v1alpha1-policy-2.yaml
  --service services.yaml --meshConfigFile meshConfig.yaml > v1beta1-authz.yaml
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			converter, err := newConverter(policyFiles, serviceFiles, istioNamespace, istioMeshConfigMapName)
			if err != nil {
				return err
			}
			err = converter.ConvertV1alpha1ToV1beta1()
			if err != nil {
				return err
			}
			writer := cmd.OutOrStdout()
			_, err = writer.Write([]byte(converter.ConvertedPolicies.String()))
			if err != nil {
				return fmt.Errorf("failed writing config: %v", err)
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

func newConverter(v1PolicyFiles, serviceFiles []string, istioNamespace, istioMeshConfigMapName string) (*converter.Converter, error) {
	if len(v1PolicyFiles) == 0 {
		return nil, fmt.Errorf("no input file provided")
	}

	var k8sClient *kubernetes2.Clientset
	k8sClient, err := kube.CreateClientset("", "")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Kubernetes: %v", err)
	}
	return converter.New(k8sClient, v1PolicyFiles, serviceFiles, meshConfig, istioNamespace, istioMeshConfigMapName)
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

  # Convert the v1alpha1 RBAC policies in the current cluster to v1beta1 authorization policies:
  istioctl x authz convert > v1beta1-authz.yaml
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
		"Check the Envoy config dump from a file")
	convertCmd.PersistentFlags().StringSliceVarP(&policyFiles, "file", "f", []string{},
		"v1alpha1 RBAC policy that needs to be converted to v1beta1 authorization policy")
	convertCmd.PersistentFlags().StringSliceVarP(&serviceFiles, "service", "s", []string{},
		"Kubernetes Service resource that provides the mapping between service and workload")
	convertCmd.PersistentFlags().StringVarP(&meshConfig, "meshConfigFile", "m", "",
		"Istio MeshConfig file that provides the root namespace value")
	convertCmd.PersistentFlags().StringVar(&istioMeshConfigMapName, "meshConfigMapName", "istio",
		"ConfigMap name for Istio mesh configuration")
}
