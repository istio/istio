// Copyright 2019 Istio Authors
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
	"strconv"
	"strings"
	"text/tabwriter"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s_labels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schemas"
	"istio.io/pkg/log"
)

type qualifiedRoute struct {
	URL     string
	Hosts   []string
	Gateway string
	Notes   string
}

var (
	gatewayName string
)

func gatewaysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gateways",
		Short: "Lists URIs exposed by gateways",
		Example: `
# Show all URIs
istioctl experimental gateways

# Just the gateways for bookinfo
istioctl x gateways --gateway bookinfo.default
`,
		Aliases: []string{"gw"},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("gateways takes no arguments")
			}
			return nil
		},
		RunE: gateways,
	}

	cmd.PersistentFlags().StringVar(&gatewayName, "gateway", "",
		"Restrict output to the named gateway")

	return cmd
}

func gateways(cmd *cobra.Command, args []string) error {
	log.Debugf("gateways command invoked")

	var configClient model.ConfigStore
	var err error
	if configClient, err = clientFactory(); err != nil {
		return err
	}
	var gateways []model.Config

	if gatewayName != "" {
		gwName, ns := handlers.InferResourceInfo(gatewayName, handlers.HandleNamespace(namespace, defaultNamespace))
		gateway := configClient.Get(schemas.Gateway.Type, gwName, ns)
		if gateway == nil {
			return fmt.Errorf("no gateway %s.%s", ns, gwName)
		}
		gateways = []model.Config{*gateway}
	} else {
		gateways, err = configClient.List(schemas.Gateway.Type, v1.NamespaceAll)
	}
	if err != nil {
		return err
	}

	writer := cmd.OutOrStdout()

	k8sClient, err := interfaceFactory(kubeconfig)
	if err != nil {
		return err
	}

	// Map gateway name to IP address
	gatewayToIP := map[string]string{}
	gatewayToPorts := map[string][]uint32{}
	gatewayToGateway := map[string]*networking.Gateway{}
	for _, gateway := range gateways {
		gatewayCfg := gateway.Spec.(*networking.Gateway)
		gatewayToGateway[gateway.Name] = gatewayCfg

		svcs, err := k8sClient.CoreV1().Services(istioNamespace).List(metav1.ListOptions{
			LabelSelector: k8s_labels.SelectorFromSet(gatewayCfg.Selector).String(),
		})
		if err != nil {
			return err
		}
		if len(svcs.Items) == 0 {
			fmt.Fprintf(writer, "Warning: Gateway %s selects no services (selector %s)\n",
				gateway.Name, k8s_labels.SelectorFromSet(gatewayCfg.Selector).String())
			continue
		}
		for _, svc := range svcs.Items {
			ip, err := getIP(svc, k8sClient)
			if err != nil {
				// TODO log
				continue
			}
			gatewayToIP[gateway.Name] = ip
			for _, server := range gatewayCfg.Servers {
				gatewayToPorts[gateway.Name] = append(gatewayToPorts[gateway.Name], server.Port.Number)
			}
		}
	}

	virtualServices, err := configClient.List(schemas.VirtualService.Type, v1.NamespaceAll)
	if err != nil {
		return err
	}

	// Identify HTTPRoutes on Virtual Services connected to gateways
	results := []qualifiedRoute{}
	for _, virtualSvc := range virtualServices {
		vsSpec := virtualSvc.Spec.(*networking.VirtualService)
		for _, vsGateway := range vsSpec.Gateways {
			ip, ok := gatewayToIP[vsGateway]
			if ok {
				if vsSpec.Http == nil {
					results = append(results, qualifiedRoute{
						URL:     ip,
						Hosts:   vsSpec.Hosts,
						Gateway: vsGateway,
						Notes:   "non-HTTP",
					})
					continue
				}

				for _, httpRoute := range vsSpec.Http {
					for _, match := range httpRoute.Match {
						results = append(results, qualifiedRoute{
							URL:     makeURL(match, ip),
							Hosts:   vsSpec.Hosts,
							Gateway: vsGateway,
							Notes:   getNotes(match, gatewayToGateway[vsGateway]),
						})
					}
				}
			}
		}
	}

	// Print identified routes
	if len(results) == 0 {
		fmt.Fprintf(writer, "No HTTP gateways found\n")
		return nil
	}
	printGateways(writer, results)

	return nil
}

func getIP(service v1.Service, k8sClient kubernetes.Interface) (string, error) {
	if len(service.Status.LoadBalancer.Ingress) > 0 {
		return service.Status.LoadBalancer.Ingress[0].IP, nil
	}

	// Get port.  See https://istio.io/docs/tasks/traffic-management/ingress/ingress-control/#determining-the-ingress-ip-and-ports
	httpPort := ""
	for _, port := range service.Spec.Ports {
		if port.Name == "http2" {
			httpPort = strconv.Itoa(int(port.NodePort))
		}
		// TODO add support for https
	}
	// TODO If there is no Ingress, get ports using
	// export INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="http2")].nodePort}')
	// export SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="https")].nodePort}')
	// TODO Get IP following https://istio.io/docs/tasks/traffic-management/ingress/ingress-control/#determining-the-ingress-ip-and-ports

	gatewayIP := "unknown"
	pods, err := k8sClient.CoreV1().Pods(istioNamespace).List(metav1.ListOptions{
		LabelSelector: "istio=ingressgateway",
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return "", multierror.Prefix(err, "Could not find NodePort Ingress pods")
	}
	if len(pods.Items) > 0 {
		gatewayIP = pods.Items[0].Status.HostIP
	}

	return fmt.Sprintf("%s:%s", gatewayIP, httpPort), nil
}

func makeURL(match *networking.HTTPMatchRequest, ip string) string {
	if match.Uri != nil {
		return fmt.Sprintf("%s%s", ip, renderStringMatch(match.Uri))
	}

	return ip
}

func getNotes(match *networking.HTTPMatchRequest, gateway *networking.Gateway) string {
	retval := []string{}
	for _, server := range gateway.Servers {
		if len(server.GetHosts()) > 1 || server.GetHosts()[0] != "*" {
			retval = append(retval, strings.Join(server.GetHosts(), " "))
		}
	}
	if match.Scheme != nil {
		retval = append(retval, "restricted Scheme")
	}
	if match.Method != nil {
		retval = append(retval, "restricted Method")
	}
	if match.Method != nil {
		retval = append(retval, "restricted Authority")
	}
	if len(match.Headers) > 0 {
		retval = append(retval, "restricted Headers")
	}
	return strings.Join(retval, "/")
}

func printGateways(writer io.Writer, routes []qualifiedRoute) {
	var w tabwriter.Writer
	w.Init(writer, 10, 4, 3, ' ', 0)
	fmt.Fprintf(&w, "URL\tHOSTS\tGATEWAY\tNOTES\n")
	for _, route := range routes {
		fmt.Fprintf(&w, "%s\t%s\t%s\t%s\n",
			route.URL, strings.Join(route.Hosts, " "), route.Gateway, route.Notes)
	}
	w.Flush() // nolint: errcheck
}
