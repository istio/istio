// Copyright Istio Authors
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
	"context"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/authz"
	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

var (
	printAll       bool
	configDumpFile string
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
	kubeClient, err := kube.NewExtendedClient(kube.BuildClientCmd(kubeconfig, configContext), "")
	if err != nil {
		return nil, err
	}

	pods, err := kubeClient.GetIstioPods(context.TODO(), podNamespace, map[string]string{
		"fieldSelector": "metadata.name=" + podName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %s", err)
	}
	if len(pods) != 1 {
		return nil, fmt.Errorf("expecting only 1 pod for %s.%s, found: %d", podName, podNamespace, len(pods))
	}

	data, err := kubeClient.EnvoyDo(context.TODO(), podName, podNamespace, "GET", "config_dump", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get proxy config for %s.%s: %s", podName, podNamespace, err)
	}
	envoyConfig := &configdump.Wrapper{}
	if err := envoyConfig.UnmarshalJSON(data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal proxy config: %s", err)
	}
	return envoyConfig, nil
}

// AuthZ groups commands used for inspecting and interacting the authorization policy.
// Note: this is still under active development and is not ready for real use.
func AuthZ() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "authz",
		Short: "Inspect and interact with authorization policies",
		Long: `Commands to inspect and interact with the authorization policies
  check - check Envoy config dump for authorization configuration
`,
		Example: `  # Check Envoy authorization configuration for pod httpbin-88ddbcfdd-nt5jb:
  istioctl x authz check httpbin-88ddbcfdd-nt5jb
`,
	}

	cmd.AddCommand(checkCmd)
	return cmd
}

func init() {
	checkCmd.PersistentFlags().BoolVarP(&printAll, "all", "a", false,
		"Show additional information (e.g. SNI and ALPN)")
	checkCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"The json file with Envoy config dump to be checked")
}
