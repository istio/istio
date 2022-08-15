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
	"fmt"
	"io"
	"strings"

	envoy_corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/istioctl/pkg/writer/pilot"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/kube"
)

func HandlerForRetrieveDebugList(kubeClient kube.ExtendedClient,
	centralOpts clioptions.CentralControlPlaneOptions,
	writer io.Writer,
) (map[string]*xdsapi.DiscoveryResponse, error) {
	var namespace, serviceAccount string
	xdsRequest := xdsapi.DiscoveryRequest{
		ResourceNames: []string{"list"},
		Node: &envoy_corev3.Node{
			Id: "debug~0.0.0.0~istioctl~cluster.local",
		},
		TypeUrl: v3.DebugType,
	}
	xdsResponses, respErr := multixds.AllRequestAndProcessXds(&xdsRequest, centralOpts, istioNamespace,
		namespace, serviceAccount, kubeClient)
	if respErr != nil {
		return xdsResponses, respErr
	}
	_, _ = fmt.Fprint(writer, "error: according to below command list, please check all supported internal debug commands\n")
	return xdsResponses, nil
}

func HandlerForDebugErrors(kubeClient kube.ExtendedClient,
	centralOpts *clioptions.CentralControlPlaneOptions,
	writer io.Writer,
	xdsResponses map[string]*xdsapi.DiscoveryResponse,
) (map[string]*xdsapi.DiscoveryResponse, error) {
	for _, response := range xdsResponses {
		for _, resource := range response.Resources {
			eString := string(resource.Value)
			switch {
			case strings.Contains(eString, "You must provide a proxyID in the query string"):
				return nil, fmt.Errorf(" You must provide a proxyID in the query string, e.g. [%s]",
					"edsz?proxyID=istio-ingressgateway")

			case strings.Contains(eString, "404 page not found"):
				return HandlerForRetrieveDebugList(kubeClient, *centralOpts, writer)

			case strings.Contains(eString, "querystring parameter 'resource' is required"):
				return nil, fmt.Errorf("querystring parameter 'resource' is required, e.g. [%s]",
					"config_distribution?resource=VirtualService/default/bookinfo")
			}
		}
	}
	return nil, nil
}

func debugCommand() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var centralOpts clioptions.CentralControlPlaneOptions

	debugCommand := &cobra.Command{
		Use:   "internal-debug [<type>/]<name>[.<namespace>]",
		Short: "Retrieves the debug information of istio",
		Long: `
Retrieves the debug information from Istiod or Pods in the mesh using the service account from the pod if --cert-dir is empty.
By default it will use the default serviceAccount from (istio-system) namespace if the pod is not specified.
`,
		Example: `  # Retrieve sync status for all Envoys in a mesh
  istioctl x internal-debug syncz

  # Retrieve sync diff for a single Envoy and Istiod
  istioctl x internal-debug syncz istio-egressgateway-59585c5b9c-ndc59.istio-system

  # SECURITY OPTIONS

  # Retrieve syncz debug information directly from the control plane, using token security
  # (This is the usual way to get the debug information with an out-of-cluster control plane.)
  istioctl x internal-debug syncz --xds-address istio.cloudprovider.example.com:15012

  # Retrieve syncz debug information via Kubernetes config, using token security
  # (This is the usual way to get the debug information with an in-cluster control plane.)
  istioctl x internal-debug syncz

  # Retrieve syncz debug information directly from the control plane, using RSA certificate security
  # (Certificates must be obtained before this step.  The --cert-dir flag lets istioctl bypass the Kubernetes API server.)
  istioctl x internal-debug syncz --xds-address istio.example.com:15012 --cert-dir ~/.istio-certs

  # Retrieve syncz information via XDS from specific control plane in multi-control plane in-cluster configuration
  # (Select a specific control plane in an in-cluster canary Istio configuration.)
  istioctl x internal-debug syncz --xds-label istio.io/rev=default
`,
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return CommandParseError{
					e: fmt.Errorf("debug type is required"),
				}
			}
			var xdsRequest xdsapi.DiscoveryRequest
			var namespace, serviceAccount string

			xdsRequest = xdsapi.DiscoveryRequest{
				ResourceNames: []string{args[0]},
				Node: &envoy_corev3.Node{
					Id: "debug~0.0.0.0~istioctl~cluster.local",
				},
				TypeUrl: v3.DebugType,
			}

			xdsResponses, err := multixds.MultiRequestAndProcessXds(internalDebugAllIstiod, &xdsRequest, centralOpts, istioNamespace,
				namespace, serviceAccount, kubeClient)
			if err != nil {
				return err
			}
			sw := pilot.XdsStatusWriter{
				Writer:                 c.OutOrStdout(),
				InternalDebugAllIstiod: internalDebugAllIstiod,
			}
			newResponse, err := HandlerForDebugErrors(kubeClient, &centralOpts, c.OutOrStdout(), xdsResponses)
			if err != nil {
				return err
			}
			if newResponse != nil {
				return sw.PrintAll(newResponse)
			}

			return sw.PrintAll(xdsResponses)
		},
	}

	opts.AttachControlPlaneFlags(debugCommand)
	centralOpts.AttachControlPlaneFlags(debugCommand)
	debugCommand.Long += "\n\n" + ExperimentalMsg
	debugCommand.PersistentFlags().BoolVar(&internalDebugAllIstiod, "all", false,
		"Send the same request to all instances of Istiod. Only applicable for in-cluster deployment.")
	return debugCommand
}

var internalDebugAllIstiod bool
