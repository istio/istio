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
	k8s "k8s.io/client-go/kubernetes"

	"istio.io/istio/istioctl/pkg/auth"
	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

var (
	printAll       bool
	configDumpFile string
	v1PolicyFile   string
	serviceFiles   []string

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
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var configDump *configdump.Wrapper
			var err error
			if configDumpFile != "" {
				configDump, err = getConfigDumpFromFile(configDumpFile)
				if err != nil {
					return fmt.Errorf("failed to get config dump from file %s: %s", configDumpFile, err)
				}
			} else if len(args) == 1 {
				podName, podNamespace := inferPodInfo(args[0], handleNamespace())
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

	upgradeCmd = &cobra.Command{
		Hidden: true,
		Use:    "upgrade",
		Short:  "Upgrade Istio Authorization Policy from version v1 to v2",
		Long: `Upgrade converts Istio authorization policy from version v1 to v2. It requires access to Kubernetes
service definition in order to translate the service name specified in the ServiceRole to the corresponding
workload labels in the AuthorizationPolicy. The service definition could be provided either from the current
Kubernetes cluster or from a yaml file specified from command line.

THIS COMMAND IS STILL UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.
`,
		Example: `  # Upgrade the Istio authorization policy with service definition from the current k8s cluster:
  istioctl experimental auth upgrade -f istio-authz-v1-policy.yaml

  # Upgrade the Istio authorization policy with service definition from 2 yaml files specified in the command line:
  istioctl experimental auth upgrade -f istio-authz-v1-policy.yaml --service svc-a.yaml,svc-b.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			upgrader, err := newUpgrader(v1PolicyFile, serviceFiles)
			if err != nil {
				return err
			}
			err = upgrader.UpgradeCRDs()
			if err != nil {
				return err
			}
			writer := cmd.OutOrStdout()
			_, err = writer.Write([]byte(upgrader.ConvertedPolicies.String()))
			if err != nil {
				return fmt.Errorf("failed writing config with error %v", err)
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

func newUpgrader(v1PolicyFile string, serviceFiles []string) (*auth.Upgrader, error) {
	if v1PolicyFile == "" {
		return nil, fmt.Errorf("no input file provided")
	}

	var k8sClient *k8s.Clientset
	var err error
	if len(serviceFiles) == 0 {
		k8sClient, err = kube.CreateClientset("", "")
		if err != nil {
			return nil, fmt.Errorf("failed to connect to Kubernetes with error %v", err)
		}
	}

	upgrader := &auth.Upgrader{
		K8sClient:                          k8sClient,
		ServiceFiles:                       serviceFiles,
		NamespaceToServiceToWorkloadLabels: map[string]auth.ServiceToWorkloadLabels{},
		V1PolicyFile:                       v1PolicyFile,
	}
	return upgrader, nil
}

// Auth groups commands used for checking the authentication and authorization policy status.
// Note: this is still under active development and is not ready for real use.
func Auth() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect and interact with authentication and authorization policies in the mesh",
		Long: `Commands to inspect and interact with the authentication (TLS, JWT) and authorization (RBAC) policies in the mesh
  check - check the TLS/JWT/RBAC settings based on the Envoy config
  upgrade - upgrade the authorization policy from version v1 to v2
`,
		Example: `  # Check the TLS/JWT/RBAC settings for pod httpbin-88ddbcfdd-nt5jb:
  istioctl experimental auth check httpbin-88ddbcfdd-nt5jb`,
	}

	cmd.AddCommand(checkCmd)
	cmd.AddCommand(upgradeCmd)
	return cmd
}

func init() {
	checkCmd.PersistentFlags().BoolVarP(&printAll, "all", "a", false,
		"Show additional information (e.g. SNI and ALPN)")
	checkCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Check the TLS/JWT/RBAC setting from the config dump file")
	upgradeCmd.PersistentFlags().StringVarP(&v1PolicyFile, "file", "f", "",
		"Authorization policy file")
	upgradeCmd.PersistentFlags().StringSliceVarP(&serviceFiles, "service", "s", []string{},
		"Kubernetes Service resource that provides the mapping relationship between service name and pod labels")
}
