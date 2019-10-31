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

	"istio.io/pkg/log"

	"istio.io/istio/istioctl/pkg/auth"
	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube"
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
		Short: "Check the TLS/JWT/RBAC settings based on Envoy config",
		Long: `Check analyzes the TLS/JWT/RBAC settings directly based on the Envoy config. The Envoy config could
be provided either by pod name or from a config dump file (the whole output of http://localhost:15000/config_dump
of an Envoy instance).

Currently only the listeners with node IP and clusters on outbound direction are analyzed:
- listeners with node IP generally tell how should other pods talk to the Envoy instance which include
  the server side TLS/JWT/RBAC settings.

- clusters on outbound direction generally tell how should the Envoy instance talk to other pods which
  include the client side TLS settings.

To check the TLS setting, you could run 'check' on both of the client and server pods and compare
the cluster results of the client pod and the listener results of the server pod.

To check the JWT/RBAC setting, you could run 'check' only on your server pods and check the listener results.

THIS COMMAND IS STILL UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.
`,
		Example: `  # Check the TLS/JWT/RBAC policy status for pod httpbin-88ddbcfdd-nt5jb in namespace foo:
  istioctl experimental auth check httpbin-88ddbcfdd-nt5jb.foo

  # Check the TLS/JWT/RBAC policy status from a config dump file:
  istioctl experimental auth check -f httpbin_config_dump.txt`,
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

			analyzer, err := auth.NewAnalyzer(configDump)
			if err != nil {
				return err
			}
			analyzer.PrintTLS(cmd.OutOrStdout(), printAll)
			return nil
		},
	}

	convertCmd = &cobra.Command{
		Use:   "convert",
		Short: "Convert v1alpha1 RBAC policy to v1beta1 authorization policy",
		Long: `Convert converts Istio v1alpha1 RBAC policy to v1beta1 authorization policy. The command talks to Kubernetes
API server to get all the information needed to complete the conversion, including the currently applied v1alpha1
RBAC policies, the Istio config-map for root namespace configuration and the k8s Service translating the
service name to workload selector.

The tool could also be used in offline mode without talking to the Kubernetes API server. In this mode,
all needed information are provided though command line.

The conversion result is printed to the stdout, you can take a final review of the converted policies
before applying it.

THIS COMMAND IS STILL UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.
`,
		Example: `  # Convert the v1alpha1 RBAC policy applied in the current cluster:
  istioctl experimental auth convert

  # Convert the v1alpha1 RBAC policy provided through command line: 
  istioctl experimental auth convert -f v1alpha1-policy-1.yaml,v1alpha1-policy-2.yaml
  --service services.yaml --meshConfigFile meshConfig.yaml
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			upgrader, err := newUpgrader(policyFiles, serviceFiles, istioNamespace, istioMeshConfigMapName)
			if err != nil {
				return err
			}
			err = upgrader.ConvertV1alpha1ToV1beta1()
			if err != nil {
				return err
			}
			writer := cmd.OutOrStdout()
			_, err = writer.Write([]byte(upgrader.ConvertedPolicies.String()))
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

	validatorCmd = &cobra.Command{
		Use:   "validate <policy-file1,policy-file2,...>",
		Short: "Validate authentication and authorization policy",
		Long: `This command goes through all authorization policy files and finds potential issues such as:
						* ServiceRoleBinding refers to a non existing ServiceRole.
						* ServiceRole not used.
           It does not require access to the cluster as the validation is against local files.
					`,
		Example: "istioctl experimental auth validate -f policy1.yaml,policy2.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			validator, err := newValidator(policyFiles)
			if err != nil {
				return err
			}
			err = validator.CheckAndReport()
			if err != nil {
				return err
			}
			writer := cmd.OutOrStdout()
			_, err = writer.Write([]byte(validator.Report.String()))
			if err != nil {
				return fmt.Errorf("failed to write report: %v", err)
			}
			return nil
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

func newUpgrader(v1PolicyFiles, serviceFiles []string, istioNamespace, istioMeshConfigMapName string) (*auth.Upgrader, error) {
	if len(v1PolicyFiles) == 0 {
		return nil, fmt.Errorf("no input file provided")
	}

	var k8sClient *kubernetes2.Clientset
	k8sClient, err := kube.CreateClientset("", "")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Kubernetes: %v", err)
	}
	upgrader, err := auth.NewUpgrader(k8sClient, v1PolicyFiles, serviceFiles, meshConfig, istioNamespace, istioMeshConfigMapName)
	if err != nil {
		return nil, err
	}
	return upgrader, nil
}

func newValidator(policyFiles []string) (*auth.Validator, error) {
	if len(policyFiles) == 0 {
		return nil, fmt.Errorf("no input file provided")
	}
	validator := &auth.Validator{
		PolicyFiles:          policyFiles,
		RoleKeyToServiceRole: make(map[string]model.Config),
	}
	return validator, nil
}

// Auth groups commands used for checking the authentication and authorization policy status.
// Note: this is still under active development and is not ready for real use.
func Auth() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect and interact with authentication and authorization policies in the mesh",
		Long: `Commands to inspect and interact with the authentication (TLS, JWT) and authorization (RBAC) policies in the mesh
  check - check the TLS/JWT/RBAC settings based on the Envoy config
  convert - convert v1alpha1 RBAC policies to v1beta1 authorization policies
	validate - check for potential incorrect usage in authorization policy files.
`,
		Example: `  # Check the TLS/JWT/RBAC settings for pod httpbin-88ddbcfdd-nt5jb:
  istioctl experimental auth check httpbin-88ddbcfdd-nt5jb

  # Upgrade the v1alpha1 RBAC policies to v1beta1 authorization policies in the cluster:
  istioctl experimental auth convert
`,
	}

	cmd.AddCommand(checkCmd)
	cmd.AddCommand(validatorCmd)
	cmd.AddCommand(convertCmd)
	return cmd
}

func init() {
	checkCmd.PersistentFlags().BoolVarP(&printAll, "all", "a", false,
		"Show additional information (e.g. SNI and ALPN)")
	checkCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Check the TLS/JWT/RBAC setting from the config dump file")
	validatorCmd.PersistentFlags().StringSliceVarP(&policyFiles, "file", "f", []string{},
		"Authorization policy file")
	convertCmd.PersistentFlags().StringSliceVarP(&policyFiles, "file", "f", []string{},
		"v1alpha1 RBAC policy that needs to be converted to v1beta1 authorization policy")
	convertCmd.PersistentFlags().StringSliceVarP(&serviceFiles, "service", "s", []string{},
		"Kubernetes Service resource that provides the mapping between service and workload")
	convertCmd.PersistentFlags().StringVarP(&meshConfig, "meshConfigFile", "m", "",
		"Istio MeshConfig file that provides the root namespace value")
	convertCmd.PersistentFlags().StringVar(&istioMeshConfigMapName, "meshConfigMapName", "istio",
		"ConfigMap name for Istio mesh configuration")
}
