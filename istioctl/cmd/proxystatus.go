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

	envoy_corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/istioctl/pkg/writer/compare"
	"istio.io/istio/istioctl/pkg/writer/pilot"
	pilotxds "istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

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
					Node: &envoy_corev3.Node{
						Id: "debug~0.0.0.0~istioctl~cluster.local",
					},
					TypeUrl: pilotxds.TypeDebugConfigDump,
				}
				xdsResponses, err := multixds.FirstRequestAndProcessXds(&xdsRequest, &centralOpts, istioNamespace, kubeClient)
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
				Node: &envoy_corev3.Node{
					Id: "debug~0.0.0.0~istioctl~cluster.local",
				},
				TypeUrl: pilotxds.TypeDebugSyncronization,
			}
			xdsResponses, err := multixds.AllRequestAndProcessXds(&xdsRequest, &centralOpts, istioNamespace, kubeClient)
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
