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
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	envoy_corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	xdsstatus "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"github.com/golang/protobuf/ptypes"
	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/istioctl/pkg/writer/compare"
	"istio.io/istio/istioctl/pkg/writer/pilot"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
	istioVersion "istio.io/pkg/version"
)

func statusCommand() *cobra.Command {
	var opts clioptions.ControlPlaneOptions

	statusCmd := &cobra.Command{
		Use:   "proxy-status [<pod-name[.namespace]>]",
		Short: "Retrieves the synchronization status of each Envoy in the mesh [kube only]",
		Long: `
Retrieves last sent and last acknowledged xDS sync from Istiod to each Envoy in the mesh

`,
		Example: `# Retrieve sync status for all Envoys in a mesh
	istioctl proxy-status

# Retrieve sync diff for a single Envoy and Istiod
	istioctl proxy-status istio-egressgateway-59585c5b9c-ndc59.istio-system
`,
		Aliases: []string{"ps"},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
			if err != nil {
				return err
			}
			if len(args) > 0 {
				podName, ns := handlers.InferPodInfo(args[0], handlers.HandleNamespace(namespace, defaultNamespace))
				path := "config_dump"
				envoyDump, err := kubeClient.EnvoyDo(context.TODO(), podName, ns, "GET", path, nil)
				if err != nil {
					return err
				}

				path = fmt.Sprintf("/debug/config_dump?proxyID=%s.%s", podName, ns)
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
			sw := pilot.StatusWriter{Writer: c.OutOrStdout()}
			return sw.PrintAll(statuses)
		},
	}

	opts.AttachControlPlaneFlags(statusCmd)

	return statusCmd
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
		Use:   "proxy-status [<pod-name[.namespace]>]",
		Short: "Retrieves the synchronization status of each Envoy in the mesh",
		Long: `
Retrieves last sent and last acknowledged xDS sync from Istiod to each Envoy in the mesh

`,
		Example: `# Retrieve sync status for all Envoys in a mesh
	istioctl proxy-status

# Retrieve sync diff for a single Envoy and Istiod
	istioctl proxy-status istio-egressgateway-59585c5b9c-ndc59.istio-system
`,
		Aliases: []string{"ps"},
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("status of single pod unimplemented") // TODO
			}

			xdsRequest := xdsapi.DiscoveryRequest{
				Node: &envoy_corev3.Node{
					Id: "debug~0.0.0.0~istioctl~cluster.local",
				},
				TypeUrl: "istio.io/debug/syncz",
			}
			kubeClient, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
			if err != nil {
				return err
			}
			// TODO Currently multixds doesn't reveal all Pod IDs, it merges them.  Don't do that.
			xdsResponse, err := multixds.RequestAndProcessXds(&xdsRequest, &centralOpts, istioNamespace, kubeClient)
			if err != nil {
				return err
			}
			if len(xdsResponse.Resources) == 0 {
				return fmt.Errorf("no proxies found on ")
			}
			return printAllStatuses(xdsResponse, c.OutOrStdout())
		},
	}

	opts.AttachControlPlaneFlags(statusCmd)
	centralOpts.AttachControlPlaneFlags(statusCmd)

	return statusCmd
}

func printAllStatuses(dr *xdsapi.DiscoveryResponse, writer io.Writer) error {
	w := new(tabwriter.Writer).Init(writer, 0, 8, 5, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tCDS\tLDS\tEDS\tRDS\tISTIOD\tVERSION")
	for _, resource := range dr.Resources {
		switch resource.TypeUrl {
		case "type.googleapis.com/envoy.service.status.v3.ClientConfig":
			clientConfig := xdsstatus.ClientConfig{}
			err := ptypes.UnmarshalAny(resource, &clientConfig)
			if err != nil {
				return fmt.Errorf("could not unmarshal ClientConfig: %w", err)
			}
			name := clientConfig.GetNode().GetId()
			cds, lds, eds, rds := getSyncStatus(clientConfig.GetXdsConfig())
			cp := cpInfo(dr)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", name, cds, lds, eds, rds, cp.ID, cp.Info.Version)
		default:
			return fmt.Errorf("/debug/syncz unexpected resource type %q", resource.TypeUrl)
		}
	}
	return w.Flush()
}

// TODO merge with code in version.go
func cpInfo(xdsResponse *xdsapi.DiscoveryResponse) xds.IstioControlPlaneInstance {
	if xdsResponse.ControlPlane == nil {
		return xds.IstioControlPlaneInstance{
			Component: "MISSING",
			ID:        "MISSING",
			Info: istioVersion.BuildInfo{
				Version: "MISSING CP ID",
			},
		}
	}

	cpID := xds.IstioControlPlaneInstance{}
	err := json.Unmarshal([]byte(xdsResponse.ControlPlane.Identifier), &cpID)
	if err != nil {
		return xds.IstioControlPlaneInstance{
			Component: "INVALID",
			ID:        "INVALID",
			Info: istioVersion.BuildInfo{
				Version: "INVALID CP ID",
			},
		}
	}
	return cpID
}

func getSyncStatus(configs []*xdsstatus.PerXdsConfig) (cds, lds, eds, rds string) {
	for _, config := range configs {
		switch val := config.PerXdsConfig.(type) {
		case *xdsstatus.PerXdsConfig_ListenerConfig:
			lds = config.Status.String()
		case *xdsstatus.PerXdsConfig_ClusterConfig:
			cds = config.Status.String()
		case *xdsstatus.PerXdsConfig_RouteConfig:
			rds = config.Status.String()
		case *xdsstatus.PerXdsConfig_EndpointConfig:
			eds = config.Status.String()
		case *xdsstatus.PerXdsConfig_ScopedRouteConfig:
			// ignore; Istiod doesn't send these
		default:
			log.Infof("PerXdsConfig unexpected type %T\n", val)
		}
	}
	return
}
