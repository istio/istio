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

	envoy_corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/istioctl/pkg/writer/pilot"
)

const (
	// TypeDebug requests debug info from istio, a secured implementation for istio debug interface
	TypeDebug = "istio.io/debug"
)

func debugCommand() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var centralOpts clioptions.CentralControlPlaneOptions

	debugCommand := &cobra.Command{
		Use:   "debug [<type>/]<name>[.<namespace>]",
		Short: "Retrieves the debug information of istio",
		Long: `
Retrieves the debug information from Istiod or Pods in the mesh using the service account from the pod if --cert-dir is empty.
By default it will use the default serviceAccount from (istio-system) namespace if the pod is not specified.
`,
		Example: `  # Retrieve sync status for all Envoys in a mesh
  istioctl x debug syncz 

  # Retrieve sync diff for a single Envoy and Istiod
  istioctl x debug syncz istio-egressgateway-59585c5b9c-ndc59.istio-system

  # SECURITY OPTIONS

  # Retrieve syncz debug information directly from the control plane, using token security
  # (This is the usual way to get the debug information with an out-of-cluster control plane.)
  istioctl x debug syncz --xds-address istio.cloudprovider.example.com:15012

  # Retrieve syncz debug information via Kubernetes config, using token security
  # (This is the usual way to get the debug information with an in-cluster control plane.)
  istioctl x debug syncz

  # Retrieve syncz debug information directly from the control plane, using RSA certificate security
  # (Certificates must be obtained before this step.  The --cert-dir flag lets istioctl bypass the Kubernetes API server.)
  istioctl x debug syncz --xds-address istio.example.com:15012 --cert-dir ~/.istio-certs

  # Retrieve syncz information via XDS from specific control plane in multi-control plane in-cluster configuration
  # (Select a specific control plane in an in-cluster canary Istio configuration.)
  istioctl x debug syncz --xds-label istio.io/rev=default
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
			if len(args) > 1 {
				podName, ns, err := handlers.InferPodInfoFromTypedResource(args[1],
					handlers.HandleNamespace(namespace, defaultNamespace),
					kubeClient.UtilFactory())
				if err != nil {
					return err
				}
				pod, err := kubeClient.CoreV1().Pods(ns).Get(context.TODO(), podName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				namespace = ns
				serviceAccount = pod.Spec.ServiceAccountName
				xdsRequest = xdsapi.DiscoveryRequest{
					ResourceNames: []string{fmt.Sprintf("%s?proxyID=%s.%s", args[0], podName, ns)},
					Node: &envoy_corev3.Node{
						Id: "debug~0.0.0.0~istioctl~cluster.local",
					},
					TypeUrl: TypeDebug,
				}
			} else {
				xdsRequest = xdsapi.DiscoveryRequest{
					ResourceNames: []string{args[0]},
					Node: &envoy_corev3.Node{
						Id: "debug~0.0.0.0~istioctl~cluster.local",
					},
					TypeUrl: TypeDebug,
				}
			}
			xdsResponses, err := multixds.AllRequestAndProcessXds(&xdsRequest, &centralOpts, istioNamespace,
				namespace, serviceAccount, kubeClient)
			if err != nil {
				return err
			}
			sw := pilot.XdsStatusWriter{Writer: c.OutOrStdout()}
			return sw.PrintAll(xdsResponses)
		},
	}

	opts.AttachControlPlaneFlags(debugCommand)
	centralOpts.AttachControlPlaneFlags(debugCommand)
	debugCommand.Long += "\n\n" + ExperimentalMsg
	return debugCommand
}
