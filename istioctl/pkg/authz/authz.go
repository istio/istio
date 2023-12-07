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

package authz

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/util"
	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
)

var configDumpFile string

func checkCmd(ctx cli.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check [<type>/]<name>[.<namespace>]",
		Short: "Check AuthorizationPolicy applied in the pod.",
		Long: `Check prints the AuthorizationPolicy applied to a pod by directly checking
the Envoy configuration of the pod. The command is especially useful for inspecting
the policy propagation from Istiod to Envoy and the final AuthorizationPolicy list merged
from multiple sources (mesh-level, namespace-level and workload-level).

The command also supports reading from a standalone config dump file with flag -f.`,
		Example: `  # Check AuthorizationPolicy applied to pod httpbin-88ddbcfdd-nt5jb:
  istioctl x authz check httpbin-88ddbcfdd-nt5jb

  # Check AuthorizationPolicy applied to one pod under a deployment
  istioctl x authz check deployment/productpage-v1

  # Check AuthorizationPolicy from Envoy config dump file:
  istioctl x authz check -f httpbin_config_dump.json`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("check requires only <pod-name>[.<pod-namespace>]")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %w", err)
			}
			var configDump *configdump.Wrapper
			if configDumpFile != "" {
				configDump, err = getConfigDumpFromFile(configDumpFile)
				if err != nil {
					return fmt.Errorf("failed to get config dump from file %s: %s", configDumpFile, err)
				}
			} else if len(args) == 1 {
				podName, podNamespace, err := ctx.InferPodInfoFromTypedResource(args[0], ctx.Namespace())
				if err != nil {
					return err
				}
				configDump, err = getConfigDumpFromPod(kubeClient, podName, podNamespace)
				if err != nil {
					return fmt.Errorf("failed to get config dump from pod %s in %s", podName, podNamespace)
				}
			} else {
				return fmt.Errorf("expecting pod name or config dump, found: %d", len(args))
			}

			analyzer, err := NewAnalyzer(configDump)
			if err != nil {
				return err
			}
			analyzer.Print(cmd.OutOrStdout())
			return nil
		},
	}
	cmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"The json file with Envoy config dump to be checked")
	return cmd
}

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
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	envoyConfig := &configdump.Wrapper{}
	if err := envoyConfig.UnmarshalJSON(data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal proxy config: %s", err)
	}
	return envoyConfig, nil
}

func getConfigDumpFromPod(kubeClient kube.CLIClient, podName, podNamespace string) (*configdump.Wrapper, error) {
	pods, err := kubeClient.GetIstioPods(context.TODO(), podNamespace, metav1.ListOptions{
		FieldSelector: "metadata.name=" + podName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %s", err)
	}
	if len(pods) != 1 {
		return nil, fmt.Errorf("expecting only 1 pod for %s.%s, found: %d", podName, podNamespace, len(pods))
	}

	data, err := kubeClient.EnvoyDo(context.TODO(), podName, podNamespace, "GET", "config_dump")
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
func AuthZ(ctx cli.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "authz",
		Short: "Inspect Istio AuthorizationPolicy",
	}

	cmd.AddCommand(checkCmd(ctx))
	cmd.Long += "\n\n" + util.ExperimentalMsg
	return cmd
}
