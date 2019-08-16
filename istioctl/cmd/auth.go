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

	"istio.io/pkg/log"

	"istio.io/istio/istioctl/pkg/auth"
	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pilot/pkg/model"
)

var (
	printAll       bool
	configDumpFile string
	policyFiles    []string

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

	// TODO(phillip): Add the upgrade command for the new authorization v1beta1 policy.

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
				return fmt.Errorf("failed to write report with error %v", err)
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
	validate - check for potential incorrect usage in authorization policy files.
`,
		Example: `  # Check the TLS/JWT/RBAC settings for pod httpbin-88ddbcfdd-nt5jb:
  istioctl experimental auth check httpbin-88ddbcfdd-nt5jb`,
	}

	cmd.AddCommand(checkCmd)
	cmd.AddCommand(validatorCmd)
	return cmd
}

func init() {
	checkCmd.PersistentFlags().BoolVarP(&printAll, "all", "a", false,
		"Show additional information (e.g. SNI and ALPN)")
	checkCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Check the TLS/JWT/RBAC setting from the config dump file")
	validatorCmd.PersistentFlags().StringSliceVarP(&policyFiles, "file", "f", []string{},
		"Authorization policy file")
}
