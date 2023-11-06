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

package proxystatus

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/util/ambient"
	"istio.io/istio/istioctl/pkg/writer/compare"
	"istio.io/istio/istioctl/pkg/writer/pilot"
	"istio.io/istio/pkg/log"
)

var configDumpFile string

func StatusCommand(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions

	statusCmd := &cobra.Command{
		Use:   "proxy-status [<type>/]<name>[.<namespace>]",
		Short: "Retrieves the synchronization status of each Envoy in the mesh [kube only]",
		Long: `
Retrieves last sent and last acknowledged xDS sync from Istiod to each Envoy in the mesh

`,
		Example: `  # Retrieve sync status for all Envoys in a mesh
  istioctl proxy-status

  # Retrieve sync diff for a single Envoy and Istiod
  istioctl proxy-status istio-egressgateway-59585c5b9c-ndc59.istio-system

  # Retrieve sync diff between Istiod and one pod under a deployment
  istioctl proxy-status deployment/productpage-v1

  # Write proxy config-dump to file, and compare to Istio control plane
  kubectl port-forward -n istio-system istio-egressgateway-59585c5b9c-ndc59 15000 &
  curl localhost:15000/config_dump > cd.json
  istioctl proxy-status istio-egressgateway-59585c5b9c-ndc59.istio-system --file cd.json
`,
		Aliases: []string{"ps"},
		Args: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 0) && (configDumpFile != "") {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("--file can only be used when pod-name is specified")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return err
			}
			if len(args) > 0 {
				podName, ns, err := ctx.InferPodInfoFromTypedResource(args[0], ctx.Namespace())
				if err != nil {
					return err
				}
				if ambient.IsZtunnelPod(kubeClient, podName, ns) {
					_, _ = fmt.Fprintf(c.OutOrStdout(),
						"Sync diff is not available for ztunnel pod %s.%s\n", podName, ns)
					return nil
				}
				var envoyDump []byte
				if configDumpFile != "" {
					envoyDump, err = readConfigFile(configDumpFile)
				} else {
					path := "config_dump"
					envoyDump, err = kubeClient.EnvoyDo(context.TODO(), podName, ns, "GET", path)
				}
				if err != nil {
					return err
				}

				path := fmt.Sprintf("debug/config_dump?proxyID=%s.%s", podName, ns)
				istiodDumps, err := kubeClient.AllDiscoveryDo(context.TODO(), ctx.IstioNamespace(), path)
				if err != nil {
					return err
				}
				c, err := compare.NewComparator(c.OutOrStdout(), istiodDumps, envoyDump)
				if err != nil {
					return err
				}
				return c.Diff()
			}
			queryStr := "debug/syncz"
			if ctx.Namespace() != "" {
				queryStr += "?namespace=" + ctx.Namespace()
			}
			statuses, err := kubeClient.AllDiscoveryDo(context.TODO(), ctx.IstioNamespace(), queryStr)
			if err != nil {
				return err
			}
			sw := pilot.StatusWriter{Writer: c.OutOrStdout()}
			return sw.PrintAll(statuses)
		},
	}

	opts.AttachControlPlaneFlags(statusCmd)
	statusCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")

	return statusCmd
}

func readConfigFile(filename string) ([]byte, error) {
	file := os.Stdin
	if filename != "-" {
		var err error
		file, err = os.Open(filename)
		if err != nil {
			return nil, err
		}
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
	return data, nil
}
