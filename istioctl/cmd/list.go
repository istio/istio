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
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/pkg/log"
)

type PilotEndpoint struct {
	Service  string                  `json:"svc"`
	Endpoint []model.ServiceInstance `json:"ep"`
}

type Cluster struct {
	APIServer    string
	APIServerVip string
	Port         int
	Protocol     string
	Service      string
	Status       string
}

type Pod struct {
	PodName   string
	Namespace string
	Vip       string
	Address   string
	Port      int
	PortName  string
	Protocol  string
	Locality  string
}

func list() *cobra.Command {
	registerCmd := &cobra.Command{
		Use:   "list <resource-type>...",
		Short: "List resources watched by Istio when running in Kubernetes.",
		Example: `
# Retrieve Kubernetes clusters in view of the current Istio control plane:
istioctl experimental list clusters

# Retrieve services discovered by Istio control plane
istioctl experimental list services -n foo

# Retrieve endpoints for a service discovered by Istio
istioctl experimental list endpoints bar -n foo

# Retrieve pods for a service discovered by Istio
istioctl experimental list pods bar -n foo

# Retrieve all pods for in a namespace discovered by Istio
istioctl experimental list pods -n foo
`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) (err error) {
			resource := args[0]
			log.Infof("istioctl list args: %v...", args)
			ns := handlers.HandleNamespace(namespace, defaultNamespace)
			svc := handleSvcName(args)
			if resource == "clusters" {
				err = ListClusters()
			} else if resource == "pods" {
				err = ListInjectedPods(svc, ns)
			} else if resource == "services" {
				err = ListServices(ns)
			} else if resource == "endpoints" {
				err = ListEndpoints(svc, ns)
			}
			return
		},
	}
	loggingOptions.SetOutputLevel(log.DefaultScopeName, log.ErrorLevel)
	return registerCmd
}

func handleSvcName(args []string) string {
	if len(args) > 1 {
		return args[1]
	}
	return ""
}

func ListClusters() (err error) {
	var pilots map[string][]byte
	if pilots, err = getEndpointInfo(); err != nil {
		return fmt.Errorf("failed to retrieve services from pilot /debug/endpointz")
	}
	var kubeAPIEndpoints []PilotEndpoint
	for pilotPod, pilot := range pilots {
		log.Infof("Pilot Pod: %v", pilotPod)
		var endpointz []PilotEndpoint
		if err := json.Unmarshal(pilot, &endpointz); err != nil {
			return fmt.Errorf("failed to unmarshal pilot /debug/endpointz response: %v", pilot)
		}

		for _, ep := range endpointz {
			// The apiserver is kubernetes.default.xxx.xxx.xxx:https
			if strings.HasPrefix(ep.Service, "kubernetes.default.") {
				kubeAPIEndpoints = append(kubeAPIEndpoints, ep)
			}
		}
	}

	var kubeConfig *rest.Config
	if kubeConfig, err = getCurrentClusterConfig(); err != nil {
		return fmt.Errorf("failed to get current kube config: %v", err)
	}
	log.Infof("Current K8s Http Server: %v\n", kubeConfig.Host)

	clusterList := map[Cluster]bool{}
	for _, kubeEndpoints := range kubeAPIEndpoints {
		for _, ep := range kubeEndpoints.Endpoint {
			if ep.Service == nil {
				continue
			}
			status := "Watching"
			if "https://"+ep.Endpoint.Address == kubeConfig.Host {
				status = "Current"
			}
			c := Cluster{
				APIServer:    ep.Endpoint.Address,
				APIServerVip: ep.Service.Address,
				Port:         ep.Endpoint.Port,
				Protocol:     string(ep.Endpoint.ServicePort.Protocol),
				Service:      kubeEndpoints.Service,
				Status:       status,
			}
			clusterList[c] = true
		}
	}

	writer := getWriter()
	fmt.Fprintln(writer, "CLUSTER\tAPI-SVR-IP\tAPI-SVR-VIP\tPORT\tPROTOCOL\t")

	for cluster := range clusterList {
		fmt.Fprintf(writer, "%v\t%v\t%v\t%v\t%v\t\n",
			cluster.Status,
			cluster.APIServer,
			cluster.APIServerVip,
			cluster.Port,
			cluster.Protocol,
		)
	}
	writer.Flush()
	return nil
}

func ListServices(ns string) (err error) {
	log.Infof("namespace: %v", ns)

	var pilots map[string][]byte
	if pilots, err = getServiceInfo(); err != nil {
		return fmt.Errorf("failed to retrieve services from pilot /debug/registryz")
	}

	svcList := map[string]*model.Service{}
	for pod, registry := range pilots {
		log.Infof("Pilot Pod: %v", pod)
		var serviceRegistryz []*model.Service
		if err := json.Unmarshal(registry, &serviceRegistryz); err != nil {
			return fmt.Errorf("failed to unmarshal pilot /debug/registryz response: %v", registry)
		}
		for _, svc := range serviceRegistryz {
			if svc.Hostname == "" {
				continue
			}
			if ns == "" || svc.Attributes.Namespace == ns {
				svcList[string(svc.Hostname)] = svc
			}
		}
	}

	// Output
	writer := getWriter()
	fmt.Fprintln(writer, "SERVICE\tNAMESPACE\tCLUSTER-SECRET:VIP\tPORT-NAME(s)\t")

	for _, svc := range svcList {
		var vipList []string
		for secret, vip := range svc.ClusterVIPs {
			if secret == "Kubernetes" {
				secret = "local"
			}
			vipList = append(vipList, fmt.Sprintf("%v:%v", secret, vip))
		}

		svcFullName := strings.Split(string(svc.Hostname), ".")
		if len(svcFullName) < 2 {
			continue
		}
		name, ns := svcFullName[0], svcFullName[1]

		fmt.Fprintf(writer, "%v\t%v\t%v\t%v\t\n",
			name,
			ns,
			vipList,
			svc.Ports.GetNames(),
		)
	}
	writer.Flush()
	return nil
}

func ListEndpoints(svc string, ns string) (err error) {
	log.Infof("service: %v", svc)
	log.Infof("namespace: %v", ns)

	var pilots map[string][]byte
	if pilots, err = getEndpointInfo(); err != nil {
		return fmt.Errorf("failed to retrieve services from pilot /debug/endpointz")
	}

	svcEndpoints := map[string]model.ServiceInstance{}
	for pilotPod, pilot := range pilots {
		log.Infof("Pilot Pod: %v", pilotPod)
		var endpointz []PilotEndpoint
		if err := json.Unmarshal(pilot, &endpointz); err != nil {
			return fmt.Errorf("failed to unmarshal pilot /debug/endpointz response: %v", pilot)
		}

		for _, ep := range endpointz {
			if ep.Service == "" {
				continue
			}
			if strings.HasPrefix(ep.Service, fmt.Sprintf("%v.%v.", svc, ns)) {
				for _, e := range ep.Endpoint {
					svcEndpoints[fmt.Sprintf("%v:%v:%v",
						ep.Service, e.Endpoint.Address, e.Endpoint.ServicePort)] = e
				}
			}
		}
	}

	// Output
	writer := getWriter()
	fmt.Fprintln(writer, "NAME\tNAMESPACE\tVIP\tADDR\tPORT\tPORT-NAME\tPROTOCOL\tLOCALITY\t")
	for _, ep := range svcEndpoints {
		if ep.Service == nil {
			continue
		}
		fmt.Fprintf(writer, "%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v\t\n",
			ep.Service.Attributes.Name,
			ep.Service.Attributes.Namespace,
			ep.Service.Address,
			ep.Endpoint.Address,
			ep.Endpoint.ServicePort.Port,
			ep.Endpoint.ServicePort.Name,
			ep.Endpoint.ServicePort.Protocol,
			ep.Endpoint.Locality,
		)
	}
	writer.Flush()
	return nil
}

func ListInjectedPods(svc string, ns string) (err error) {
	log.Infof("namespace: %v; service: %v", ns, svc)

	var pilots map[string][]byte
	if pilots, err = getEndpointInfo(); err != nil {
		return fmt.Errorf("failed to retrieve services from pilot /debug/endpointz")
	}

	podList := map[Pod]bool{}
	for pilotPod, pilot := range pilots {
		log.Infof("Pilot Pod: %v", pilotPod)
		var endpointz []PilotEndpoint
		if err := json.Unmarshal(pilot, &endpointz); err != nil {
			return fmt.Errorf("failed to unmarshal pilot /debug/endpointz response: %v", pilot)
		}

		for _, ep := range endpointz {
			if ep.Service == "" {
				continue
			}
			for _, endpoint := range ep.Endpoint {
				if endpoint.Service == nil {
					continue
				}
				if ns == endpoint.Service.Attributes.Namespace &&
					(svc == "" || svc == endpoint.Service.Attributes.Name) {
					podStr := strings.TrimPrefix(endpoint.Endpoint.UID, "kubernetes://")
					podFullName := strings.Split(podStr, ".")
					if len(podFullName) < 2 {
						continue
					}
					name, ns := podFullName[0], podFullName[1]
					pod := Pod{
						PodName:   name,
						Namespace: ns,
						Vip:       endpoint.Service.Address,
						Address:   endpoint.Endpoint.Address,
						Port:      endpoint.Endpoint.ServicePort.Port,
						PortName:  endpoint.Endpoint.ServicePort.Name,
						Protocol:  string(endpoint.Endpoint.ServicePort.Protocol),
						Locality:  endpoint.Endpoint.Locality,
					}
					podList[pod] = true
				}
			}
		}
	}
	// Output
	writer := getWriter()
	fmt.Fprintln(writer, "POD\tNAMESPACE\tVIP\tADDR\tPORT\tPORT-NAME\tPROTOCOL\tLOCALITY\t")

	for pod := range podList {
		fmt.Fprintf(writer, "%v\t%v\t%v\t%v\t%v\t%v\t%v\t%v\t\n",
			pod.PodName,
			pod.Namespace,
			pod.Vip,
			pod.Address,
			pod.Port,
			pod.PortName,
			pod.Protocol,
			pod.Locality,
		)
	}
	writer.Flush()
	return nil
}

func getWriter() *tabwriter.Writer {
	w := tabwriter.NewWriter(os.Stdout, 5, 1, 3, ' ', 0)
	return w
}

func getServiceInfo() (map[string][]byte, error) {
	kubeClient, err := clientExecFactory(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	return kubeClient.AllPilotsDiscoveryDo(istioNamespace,
		"GET", "/debug/registryz", nil)
}

func getEndpointInfo() (map[string][]byte, error) {
	kubeClient, err := clientExecFactory(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	return kubeClient.AllPilotsDiscoveryDo(istioNamespace,
		"GET", "/debug/endpointz", nil)
}

func getCurrentClusterConfig() (*rest.Config, error) {
	kubeClient, err := kubernetes.NewClient(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	return kubeClient.Config, err
}
