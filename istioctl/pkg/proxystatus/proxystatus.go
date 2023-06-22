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
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/istioctl/pkg/writer/compare"
	"istio.io/istio/istioctl/pkg/writer/pilot"
	pilotxds "istio.io/istio/pilot/pkg/xds"
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
			statuses, err := kubeClient.AllDiscoveryDo(context.TODO(), ctx.IstioNamespace(), "debug/syncz")
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

func XdsStatusCommand(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var centralOpts clioptions.CentralControlPlaneOptions
	var multiXdsOpts multixds.Options

	statusCmd := &cobra.Command{
		Use:   "proxy-status [<type>/]<name>[.<namespace>]",
		Short: "Retrieves the synchronization status of each Envoy in the mesh",
		Long: `
Retrieves last sent and last acknowledged xDS sync from Istiod to each Envoy in the mesh
`,
		Example: `  # Retrieve sync status for all Envoys in a mesh
  istioctl x proxy-status

  # Retrieve sync diff for a single Envoy and Istiod
  istioctl x proxy-status istio-egressgateway-59585c5b9c-ndc59.istio-system

  # SECURITY OPTIONS

  # Retrieve proxy status information directly from the control plane, using token security
  # (This is the usual way to get the proxy-status with an out-of-cluster control plane.)
  istioctl x ps --xds-address istio.cloudprovider.example.com:15012

  # Retrieve proxy status information via Kubernetes config, using token security
  # (This is the usual way to get the proxy-status with an in-cluster control plane.)
  istioctl x proxy-status

  # Retrieve proxy status information directly from the control plane, using RSA certificate security
  # (Certificates must be obtained before this step.  The --cert-dir flag lets istioctl bypass the Kubernetes API server.)
  istioctl x ps --xds-address istio.example.com:15012 --cert-dir ~/.istio-certs

  # Retrieve proxy status information via XDS from specific control plane in multi-control plane in-cluster configuration
  # (Select a specific control plane in an in-cluster canary Istio configuration.)
  istioctl x ps --xds-label istio.io/rev=default
`,
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
				var envoyDump []byte
				if configDumpFile != "" {
					envoyDump, err = readConfigFile(configDumpFile)
				} else {
					path := "config_dump"
					envoyDump, err = kubeClient.EnvoyDo(context.TODO(), podName, ns, "GET", path)
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
			sw := pilot.XdsStatusWriter{Writer: c.OutOrStdout()}
			return sw.PrintAll(xdsResponses)
		},
	}

	opts.AttachControlPlaneFlags(statusCmd)
	centralOpts.AttachControlPlaneFlags(statusCmd)
	statusCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")
	statusCmd.PersistentFlags().BoolVar(&multiXdsOpts.XdsViaAgents, "xds-via-agents", false,
		"Access Istiod via the tap service of each agent")
	statusCmd.PersistentFlags().IntVar(&multiXdsOpts.XdsViaAgentsLimit, "xds-via-agents-limit", 100,
		"Maximum number of pods being visited by istioctl when `xds-via-agent` flag is true."+
			"To iterate all the agent pods without limit, set to 0")

	return statusCmd
}
