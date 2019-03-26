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

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/auth"
	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/util/configdump"
)

var (
	printAll bool

	checkCmd = &cobra.Command{
		Use:   "check <pod-name>[.<pod-namespace>]",
		Short: "Check the TLS/JWT/RBAC setting for a pod based on its Envoy config",
		Long:  `Check the TLS/JWT/RBAC setting for a pod based on its Envoy config`,
		Example: `  # Check the TLS settings for pod httpbin-88ddbcfdd-nt5jb in namespace foo:
  istioctl experimental auth check httpbin-88ddbcfdd-nt5jb.foo`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expecting only 1 argument, found: %d", len(args))
			}
			podName, podNamespace := inferPodInfo(args[0], handleNamespace())

			analyzer, err := getAnalyzerForPod(podName, podNamespace)
			if err != nil {
				return err
			}
			analyzer.PrintTLS(cmd.OutOrStdout(), printAll)
			return nil
		},
	}
)

func getAnalyzerForPod(podName, podNamespace string) (*auth.Analyzer, error) {
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

	configDump, err := kubeClient.EnvoyDo(podName, podNamespace, "GET", "config_dump", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get proxy config for %s.%s: %s", podName, podNamespace, err)
	}
	envoyConfig := &configdump.Wrapper{}
	if err := envoyConfig.UnmarshalJSON(configDump); err != nil {
		return nil, fmt.Errorf("failed to unmarshal proxy config: %s", err)
	}

	return auth.NewAnalyzer(&pods[0], envoyConfig)
}

// Auth groups commands used for checking the authentication and authorization policy status.
// Note: this is still under active development and is not ready for real use.
func Auth() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Check authentication and authorization policy configuration and status",
		Long: `Commands to check the authentication (TLS, JWT) and authorization (RBAC) policy status in the mesh
  check - check the TLS/JWT/RBAC policy status for a pod
`,
		Example: `  # Check the TLS/JWT/RBAC settings for pod httpbin-88ddbcfdd-nt5jb in namespace foo:
  istioctl experimental auth check httpbin-88ddbcfdd-nt5jb.foo`,
	}

	cmd.AddCommand(checkCmd)
	return cmd
}

func init() {
	checkCmd.PersistentFlags().BoolVar(&printAll, "all", false, "Show additional information (e.g. SNI and ALPN)")
}
