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

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/completion"
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/istioctl/pkg/util/ambient"
	"istio.io/istio/istioctl/pkg/writer/compare"
	"istio.io/istio/istioctl/pkg/writer/pilot"
	pilotxds "istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/log"
)

var (
	proxyAdminPort int
	configDumpFile string
)

const defaultProxyAdminPort = 15000

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

func StableXdsStatusCommand(ctx cli.Context) *cobra.Command {
	cmd := XdsStatusCommand(ctx)
	unstableFlags := []string{}
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		for _, flag := range unstableFlags {
			if cmd.PersistentFlags().Changed(flag) {
				return fmt.Errorf("--%s is experimental. Use `istioctl experimental ps --%s`", flag, flag)
			}
		}
		return nil
	}
	for _, flag := range unstableFlags {
		_ = cmd.PersistentFlags().MarkHidden(flag)
	}
	return cmd
}

func XdsStatusCommand(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var centralOpts clioptions.CentralControlPlaneOptions
	var multiXdsOpts multixds.Options
	var outputFormat string
	var verbosity int

	statusCmd := &cobra.Command{
		Use:   "proxy-status [<type>/]<name>[.<namespace>]",
		Short: "Retrieves the synchronization status of each Envoy in the mesh",
		Long: `
Retrieves last sent and last acknowledged xDS sync from Istiod to each Envoy in the mesh
`,
		Example: `  # Retrieve sync status for all Envoys in a mesh
  istioctl proxy-status

  # Retrieve sync status for Envoys in a specific namespace
  istioctl proxy-status --namespace foo

  # Retrieve sync diff for a single Envoy and Istiod
  istioctl proxy-status istio-egressgateway-59585c5b9c-ndc59.istio-system

  # SECURITY OPTIONS

  # Retrieve proxy status information directly from the control plane, using token security
  # (This is the usual way to get the proxy-status with an out-of-cluster control plane.)
  istioctl ps --xds-address istio.cloudprovider.example.com:15012

  # Retrieve proxy status information via Kubernetes config, using token security
  # (This is the usual way to get the proxy-status with an in-cluster control plane.)
  istioctl proxy-status

  # Retrieve proxy status information directly from the control plane, using RSA certificate security
  # (Certificates must be obtained before this step.  The --cert-dir flag lets istioctl bypass the Kubernetes API server.)
  istioctl ps --xds-address istio.example.com:15012 --cert-dir ~/.istio-certs

  # Retrieve proxy status information via XDS from specific control plane in multi-control plane in-cluster configuration
  # (Select a specific control plane in an in-cluster canary Istio configuration.)
  istioctl ps --xds-label istio.io/rev=default

  # Show the status of a specific proxy in JSON format
  istioctl proxy-status --output json`,
		Aliases: []string{"ps"},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return err
			}
			multiXdsOpts.MessageWriter = c.OutOrStdout()

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
					envoyDump, err = kubeClient.EnvoyDoWithPort(context.TODO(), podName, ns, "GET", path, proxyAdminPort)
				}
				if err != nil {
					return fmt.Errorf("could not contact sidecar: %w", err)
				}

				xdsRequest := discovery.DiscoveryRequest{
					ResourceNames: []string{fmt.Sprintf("%s.%s", podName, ns)},
					TypeUrl:       pilotxds.TypeDebugConfigDump,
				}
				xdsResponses, err := multixds.FirstRequestAndProcessXds(&xdsRequest, centralOpts, ctx.IstioNamespace(), "", "", kubeClient, multiXdsOpts)
				if err != nil {
					return err
				}
				c, err := compare.NewXdsComparator(c.OutOrStdout(), xdsResponses, envoyDump)
				if err != nil {
					return err
				}
				return c.Diff()
			}
			xdsRequest := discovery.DiscoveryRequest{
				TypeUrl: pilotxds.TypeDebugSyncronization,
			}
			xdsResponses, err := multixds.AllRequestAndProcessXds(&xdsRequest, centralOpts, ctx.IstioNamespace(), "", "", kubeClient, multiXdsOpts)
			if err != nil {
				return err
			}
			sw := pilot.XdsStatusWriter{
				Writer:       c.OutOrStdout(),
				Namespace:    ctx.Namespace(),
				OutputFormat: outputFormat,
				Verbosity:    verbosity,
			}
			return sw.PrintAll(xdsResponses)
		},
		ValidArgsFunction: completion.ValidPodsNameArgs(ctx),
	}

	opts.AttachControlPlaneFlags(statusCmd)
	centralOpts.AttachControlPlaneFlags(statusCmd)
	statusCmd.PersistentFlags().StringVar(&configDumpFile, "file", "",
		"Envoy config dump JSON file")
	statusCmd.PersistentFlags().IntVar(&proxyAdminPort, "proxy-admin-port", defaultProxyAdminPort, "Envoy proxy admin port")

	statusCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table",
		"Output format: table or json")
	statusCmd.PersistentFlags().IntVarP(&verbosity, "verbosity", "v", 0,
		"Verbosity level for proxy status output. 0=default, 1=show all xDS types (max verbosity)")

	return statusCmd
}
