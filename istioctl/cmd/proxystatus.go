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
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/istioctl/pkg/writer/compare"
	"istio.io/istio/istioctl/pkg/writer/pilot"
	pilotxds "istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

var (
	allXDSCols = []string{
		v3.GetShortType(v3.ClusterType),
		v3.GetShortType(v3.ListenerType),
		v3.GetShortType(v3.EndpointType),
		v3.GetShortType(v3.RouteType),
		v3.GetShortType(v3.NameTableType),
		v3.GetShortType(v3.ExtensionConfigurationType),
		v3.GetShortType(v3.ProxyConfigType),
	}

	commonXDSCols = []string{
		v3.GetShortType(v3.ClusterType),
		v3.GetShortType(v3.ListenerType),
		v3.GetShortType(v3.EndpointType),
		v3.GetShortType(v3.RouteType),
	}
)

func statusCommand() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var showAllXDSStatuses bool
	var xdsStatusCols []string

	statusCmd := &cobra.Command{
		Use:   "proxy-status [<type>/]<name>[.<namespace>]",
		Short: "Retrieves the synchronization status of each Envoy in the mesh [kube only]",
		Long: `
Retrieves last sent and last acknowledged xDS sync from Istiod to each Envoy in the mesh

`,
		Example: `  # Retrieve sync status of common XDS types for all Envoys in a mesh
  istioctl proxy-status

  # Display sync statuses of all XDS types (WARNING: Depending on horizontal screen size
  # it may be split into multiple lines. In that case, filter columns with --xds-cols
  # option instead (see below)
  istioctl proxy-status -A

  # Display sync statuses of only LDS and EDS for all Envoys in a mesh
  istioctl proxy-status --xds-cols=lds,cds

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
			if showAllXDSStatuses {
				if len(xdsStatusCols) > 0 {
					return fmt.Errorf("either --all-xds-statuses or --xds-status-cols should be used, not both")
				}
				xdsStatusCols = getXDSColsList(allXDSCols)
			} else if len(xdsStatusCols) == 0 {
				xdsStatusCols = getXDSColsList(commonXDSCols)
			}
			xdsCols := getXDSColsList(allXDSCols)
			for _, xcgiven := range xdsStatusCols {
				found := false
				for _, xcvalid := range xdsCols {
					if xcgiven == xcvalid {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("invalid XDS column: %s (must be one of %s)", xcgiven,
						strings.Join(xdsCols, ","))
				}
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
			if err != nil {
				return err
			}
			if len(args) > 0 {
				podName, ns, err := handlers.InferPodInfoFromTypedResource(args[0],
					handlers.HandleNamespace(namespace, defaultNamespace),
					kubeClient.UtilFactory())
				if err != nil {
					return err
				}
				var envoyDump []byte
				if configDumpFile != "" {
					envoyDump, err = readConfigFile(configDumpFile)
				} else {
					path := "config_dump"
					envoyDump, err = kubeClient.EnvoyDo(context.TODO(), podName, ns, "GET", path, nil)
				}
				if err != nil {
					return err
				}

				path := fmt.Sprintf("/debug/config_dump?proxyID=%s.%s", podName, ns)
				istiodDumps, err := kubeClient.AllDiscoveryDo(context.TODO(), istioNamespace, path)
				if err != nil {
					return err
				}
				c, err := compare.NewComparator(c.OutOrStdout(), istiodDumps, envoyDump)
				if err != nil {
					return err
				}
				return c.Diff()
			}
			statuses, err := kubeClient.AllDiscoveryDo(context.TODO(), istioNamespace, "/debug/syncz")
			if err != nil {
				return err
			}
			sw := pilot.StatusWriter{Writer: c.OutOrStdout(), XDSCols: commonXDSCols}
			sw.XDSCols = getColumnHeaders(xdsStatusCols)
			return sw.PrintAll(statuses)
		},
	}

	opts.AttachControlPlaneFlags(statusCmd)
	statusCmd.PersistentFlags().BoolVarP(&showAllXDSStatuses, "all-xds-statuses", "A", false,
		"When true, it displays all xDS distribution statues (including NDS, ECDS, PCDS etc)")
	statusCmd.PersistentFlags().StringSliceVarP(&xdsStatusCols, "xds-cols", "", nil,
		fmt.Sprintf("The list of XDS status columns to display. Valid ones are: %s", strings.Join(getXDSColsList(allXDSCols), ",")))
	statusCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")

	return statusCmd
}

func getXDSColsList(types []string) []string {
	cols := []string{}
	for _, ty := range types {
		cols = append(cols, strings.ToLower(v3.GetShortType(ty)))
	}
	return cols
}

func getColumnHeaders(cols []string) []string {
	longTy := []string{}
	for _, c := range cols {
		longTy = append(longTy, strings.ToUpper(c))
	}
	return longTy
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
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func newKubeClientWithRevision(kubeconfig, configContext string, revision string) (kube.ExtendedClient, error) {
	return kube.NewExtendedClient(kube.BuildClientCmd(kubeconfig, configContext), revision)
}

func newKubeClient(kubeconfig, configContext string) (kube.ExtendedClient, error) {
	return newKubeClientWithRevision(kubeconfig, configContext, "")
}

func xdsStatusCommand() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var centralOpts clioptions.CentralControlPlaneOptions

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
			kubeClient, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
			if err != nil {
				return err
			}

			if len(args) > 0 {
				podName, ns, err := handlers.InferPodInfoFromTypedResource(args[0],
					handlers.HandleNamespace(namespace, defaultNamespace),
					kubeClient.UtilFactory())
				if err != nil {
					return err
				}
				path := "config_dump"
				envoyDump, err := kubeClient.EnvoyDo(context.TODO(), podName, ns, "GET", path, nil)
				if err != nil {
					return fmt.Errorf("could not contact sidecar: %w", err)
				}

				xdsRequest := xdsapi.DiscoveryRequest{
					ResourceNames: []string{fmt.Sprintf("%s.%s", podName, ns)},
					TypeUrl:       pilotxds.TypeDebugConfigDump,
				}
				xdsResponses, err := multixds.FirstRequestAndProcessXds(&xdsRequest, &centralOpts, istioNamespace, "", "", kubeClient)
				if err != nil {
					return err
				}
				c, err := compare.NewXdsComparator(c.OutOrStdout(), xdsResponses, envoyDump)
				if err != nil {
					return err
				}
				return c.Diff()
			}

			xdsRequest := xdsapi.DiscoveryRequest{
				TypeUrl: pilotxds.TypeDebugSyncronization,
			}
			xdsResponses, err := multixds.AllRequestAndProcessXds(&xdsRequest, &centralOpts, istioNamespace, "", "", kubeClient)
			if err != nil {
				return err
			}
			sw := pilot.XdsStatusWriter{Writer: c.OutOrStdout()}
			return sw.PrintAll(xdsResponses)
		},
	}

	opts.AttachControlPlaneFlags(statusCmd)
	centralOpts.AttachControlPlaneFlags(statusCmd)

	return statusCmd
}
