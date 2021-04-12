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
	"io"
	"strings"

	envoy_corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubectl/pkg/polymorphichelpers"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/istioctl/pkg/writer/pilot"
	"istio.io/istio/pkg/kube"
)

const (
	// TypeDebug requests debug info from istio, a secured implementation for istio debug interface
	TypeDebug         = "istio.io/debug"
	istiodServiceName = "istiod"
	xdsPortName       = "https-dns"
)

type DebugCommandOpts struct {
	// ResouceName is used for receiving istio CRD resource, e.g. VirtualService/default/bookinfo
	ResouceName string
}

// AttachDebugCommandFlags attaches control-plane flags to the internal-debug Cobra command.
// (Currently just --resource)
func (o *DebugCommandOpts) AttachDebugCommandFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.ResouceName, "resource", "",
		"resource can be used in format of kind/namespace/name, e.g. 'VirtualService/default/bookinfo'")
}

func PreHandleForXdsRequest(iPod, namespace string, kubeClient kube.ExtendedClient) (string, string, string, error) {
	podName, ns, err := handlers.InferPodInfoFromTypedResource(iPod,
		handlers.HandleNamespace(namespace, defaultNamespace),
		kubeClient.UtilFactory())
	if err != nil {
		return "", "", "", err
	}
	pod, perr := kubeClient.CoreV1().Pods(ns).Get(context.TODO(), podName, metav1.GetOptions{})
	if perr != nil {
		return "", "", "", perr
	}
	return podName, ns, pod.Spec.ServiceAccountName, perr
}

func HandlerForRetrieveDebugList(kubeClient kube.ExtendedClient,
	centralOpts *clioptions.CentralControlPlaneOptions,
	writer io.Writer) (map[string]*xdsapi.DiscoveryResponse, error) {
	var namespace, serviceAccount string
	xdsRequest := xdsapi.DiscoveryRequest{
		ResourceNames: []string{"list"},
		Node: &envoy_corev3.Node{
			Id: "debug~0.0.0.0~istioctl~cluster.local",
		},
		TypeUrl: TypeDebug,
	}
	xdsResponses, respErr := multixds.AllRequestAndProcessXds(&xdsRequest, centralOpts, istioNamespace,
		namespace, serviceAccount, kubeClient)
	if respErr != nil {
		return xdsResponses, respErr
	}
	_, _ = fmt.Fprint(writer, "error: according to below command list, please check all supported internal debug commands\n")
	return xdsResponses, nil
}

func HandlerForDebugErrors(cmdName string,
	kubeClient kube.ExtendedClient,
	centralOpts *clioptions.CentralControlPlaneOptions,
	writer io.Writer,
	xdsResponses map[string]*xdsapi.DiscoveryResponse) (map[string]*xdsapi.DiscoveryResponse, error) {
	for _, response := range xdsResponses {
		for _, resource := range response.Resources {
			eString := string(resource.Value)
			switch {
			case strings.Contains(eString, "You must provide a proxyID in the query string"):
				return nil, fmt.Errorf("please specify pod name with namespace, e.g. [%s] for command [%s]",
					"istio-ingressgateway-xxx-yyy.istio-system", cmdName)

			case strings.Contains(eString, "404 page not found"):
				return HandlerForRetrieveDebugList(kubeClient, centralOpts, writer)

			case strings.Contains(eString, "querystring parameter 'resource' is required"):
				return nil, fmt.Errorf("please specify resource option for command [%s], e.g. [%s]",
					cmdName, "--resource VirtualService/default/bookinfo")
			}
		}
	}
	return nil, nil
}

func debugCommand() *cobra.Command {
	var configDistributedOpt DebugCommandOpts
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
			var namespace, serviceAccount, resourceName string
			if len(args) > 1 {
				podName, ns, svcAccount, perr := PreHandleForXdsRequest(args[1], namespace, kubeClient)
				if perr != nil {
					return perr
				}
				namespace = ns
				serviceAccount = svcAccount
				resourceName = fmt.Sprintf("%s?proxyID=%s.%s", args[0], podName, ns)
				xdsRequest = xdsapi.DiscoveryRequest{
					ResourceNames: []string{resourceName},
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
			if args[0] == "config_distribution" {
				sResource := configDistributedOpt.ResouceName
				if resourceName != "" {
					resourceName += fmt.Sprintf("&resource=%s", sResource)
				} else {
					resourceName = fmt.Sprintf("%s?resource=%s", args[0], sResource)
				}
				xdsRequest = xdsapi.DiscoveryRequest{
					ResourceNames: []string{resourceName},
					Node: &envoy_corev3.Node{
						Id: "debug~0.0.0.0~istioctl~cluster.local",
					},
					TypeUrl: TypeDebug,
				}
			}
			if centralOpts.Xds == "" {
				svc, err := kubeClient.CoreV1().Services(istioNamespace).Get(context.Background(), istiodServiceName, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("please specify %q as %v", "--xds-address", err)
				}
				namespace, selector, err := polymorphichelpers.SelectorsForObject(svc)
				if err != nil {
					return fmt.Errorf("please specify %q as we cannot attach to %T: %v", "--xds-address", svc, err)
				}

				options := metav1.ListOptions{LabelSelector: selector.String()}

				podList, err := kubeClient.CoreV1().Pods(namespace).List(context.TODO(), options)
				if err != nil {
					return fmt.Errorf("please specify %q as %v", "--xds-address", err)
				}
				//  select a pod randomly to simulate current debug behavior
				pod := podList.Items[0]
				podPort := 15012
				for _, v := range svc.Spec.Ports {
					if v.Name == xdsPortName {
						podPort = v.TargetPort.IntValue()
					}
				}
				f, err := kubeClient.NewPortForwarder(pod.Name, pod.Namespace, "", 0, podPort)
				if err != nil {
					return fmt.Errorf("please specify %q as %v", "--xds-address", err)
				}
				if err := f.Start(); err != nil {
					return fmt.Errorf("please specify %q as %v", "--xds-address", err)
				}
				centralOpts.Xds = f.Address()
				defer func() {
					f.Close()
					f.WaitForStop()
				}()
			}
			xdsResponses, respErr := multixds.AllRequestAndProcessXds(&xdsRequest, &centralOpts, istioNamespace,
				namespace, serviceAccount, kubeClient)
			if respErr != nil {
				return respErr
			}
			sw := pilot.XdsStatusWriter{Writer: c.OutOrStdout()}
			newResponse, hDErr := HandlerForDebugErrors(args[0], kubeClient, &centralOpts, c.OutOrStdout(), xdsResponses)
			if newResponse != nil {
				return sw.PrintAll(newResponse)
			}
			if hDErr != nil {
				return hDErr
			}
			return sw.PrintAll(xdsResponses)
		},
	}

	configDistributedOpt.AttachDebugCommandFlags(debugCommand)
	opts.AttachControlPlaneFlags(debugCommand)
	centralOpts.AttachControlPlaneFlags(debugCommand)
	debugCommand.Long += "\n\n" + ExperimentalMsg
	return debugCommand
}
