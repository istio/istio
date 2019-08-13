// Copyright 2017 Istio Authors
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
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"

	"istio.io/pkg/log"
	"istio.io/pkg/version"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pilot/pkg/model"
)

type PilotEndpoint struct {
	Service  string                  `json:"svc"`
	Endpoint []model.ServiceInstance `json:"ep"`
}

type Cluster struct {
	ApiServer string
	Port      string
	Protocol  string
	Service   string
	Status    string
	Locality  string
}

type Pod struct {
	PodName            string
	Address            string
	Locality           string
}

func list() *cobra.Command {
	registerCmd := &cobra.Command{
		Use:   "list <resource-type>",
		Short: "List resources watched by Istio",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			resource := args[0]
			log.Infof("istioctl list args: %v...", args)
			ns := handlers.HandleNamespace(namespace, defaultNamespace)
			if resource == "clusters" {
				ListClusters()
			} else if resource == "pods" {
				ListInjectedPods(ns)
			} else if resource == "services" {
				ListServices(ns)
			} else if resource == "endpoints" {
				svc := args[1]
				ListEndpoints(svc, ns)
			} else if resource == "routes" {
			}
			return nil
		},
	}
	return registerCmd
}

func ListRoutingRules(svc string, ns string) error {

	return nil
}

func ListInjectedPods(ns string) (err error) {
	log.Infof("namespace: %v", ns)

	var pilots map[string][]byte
	if pilots, err = getEndpointInfo(); err != nil {
		return fmt.Errorf("failed to retrieve services from pilot /debug/endpointz")
	}

	var podList []Pod
	for pilotPod, pilot := range pilots {
		log.Infof("Pilot Pod: %v", pilotPod)
		var endpointz []PilotEndpoint
		if err := json.Unmarshal([]byte(pilot), &endpointz); err != nil {
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
				if endpoint.Service.Attributes.Namespace == ns {
					pod := Pod{
						PodName:           strings.TrimPrefix(endpoint.Endpoint.UID, "kubernetes://"),
						Address:           endpoint.Service.Address,
						Locality:          endpoint.Endpoint.Locality,
					}
					podList = append(podList, pod)
				}
			}
		}
	}
	// Output
	writer := getWriter()
	fmt.Fprintln(writer, "POD\tADDR\tLOCALITY\t")

	for _, pod := range podList {
		fmt.Fprintf(writer, "%v\t%v\t%v\t\n",
			pod.PodName,
			pod.Address,
			pod.Locality,
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

	var svcList []*model.Service
	for pod, registry := range pilots {
		log.Infof("Pilot Pod: %v", pod)
		var serviceRegistryz []*model.Service
		if err := json.Unmarshal([]byte(registry), &serviceRegistryz); err != nil {
			return fmt.Errorf("failed to unmarshal pilot /debug/registryz response: %v", registry)
		}
		for _, svc := range serviceRegistryz {
			if svc.Hostname == "" {
				continue
			}
			if ns == "" || svc.Attributes.Namespace == ns {
				svcList = append(svcList, svc)
			}
		}
	}

	// Output
	writer := getWriter()
	fmt.Fprintln(writer, "SERVICE\tVIP\tPORT NAME\t")

	for _, svc := range svcList {
		var vipList []string
		for _, vip := range svc.ClusterVIPs {
			vipList = append(vipList, vip)
		}
		fmt.Fprintf(writer, "%v\t%v\t%v\t\n",
			svc.Hostname,
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

	var svcEndpoints []PilotEndpoint
	for pilotPod, pilot := range pilots {
		log.Infof("Pilot Pod: %v", pilotPod)
		var endpointz []PilotEndpoint
		if err := json.Unmarshal([]byte(pilot), &endpointz); err != nil {
			return fmt.Errorf("failed to unmarshal pilot /debug/endpointz response: %v", pilot)
		}

		for _, ep := range endpointz {
			if ep.Service == "" {
				continue
			}
			if strings.HasPrefix(ep.Service, fmt.Sprintf("%v.%v.", svc, ns)) {
				svcEndpoints = append(svcEndpoints, ep)
			}
		}
	}

	// Output
	writer := getWriter()
	fmt.Fprintln(writer, "NAME\tSERVICE ADDR\tENDPOINT ADDR\tPORT\tLOCALITY\tLB WEIGHT\t")

	for _, svcEp := range svcEndpoints {
		for _, ep := range svcEp.Endpoint {
			if ep.Service == nil {
				continue
			}
			fmt.Fprintf(writer, "%v\t%v\t%v\t%v\t%v\t%v\t\n",
				svcEp.Service,
				ep.Service.Address,
				ep.Endpoint.Address,
				ep.Endpoint.Port,
				ep.Endpoint.Locality,
				ep.Endpoint.LbWeight,
			)
		}
	}
	writer.Flush()
	return nil
}

func ListClusters() (err error) {
	var pilots map[string][]byte
	if pilots, err = getEndpointInfo(); err != nil {
		return fmt.Errorf("failed to retrieve services from pilot /debug/endpointz")
	}
	var kubeApiEndpoints []PilotEndpoint
	var kubeHttpEndpoints []PilotEndpoint
	for pilotPod, pilot := range pilots {
		log.Infof("Pilot Pod: %v", pilotPod)
		var endpointz []PilotEndpoint
		if err := json.Unmarshal([]byte(pilot), &endpointz); err != nil {
			return fmt.Errorf("failed to unmarshal pilot /debug/endpointz response: %v", pilot)
		}

		for _, ep := range endpointz {
			// In GKE, the apiserver is kubernetes.default.svc.cluster.local:https
			if strings.HasPrefix(ep.Service, "kubernetes.default.") {
				kubeApiEndpoints = append(kubeApiEndpoints, ep)
			}
			// In GKE, the http-backend is kubernetes.default.svc.cluster.local:https
			if strings.HasPrefix(ep.Service, "default-http-backend.kube-system") {
				kubeHttpEndpoints = append(kubeHttpEndpoints, ep)
			}
		}
	}

	var kubeConfig *rest.Config
	if kubeConfig, err = getCurrentClusterConfig(); err != nil {
		return fmt.Errorf("failed to get current kube config: %v", err)
	}
	log.Infof("Current K8s Http Server: %v\n", kubeConfig.Host)

	var clusterList []Cluster
	for indexEp, kubeEndpoints := range kubeApiEndpoints {
		for i, ep := range kubeEndpoints.Endpoint {
			if ep.Service == nil {
				continue
			}
			status := "Watching"
			if "https://"+ep.Endpoint.Address == kubeConfig.Host {
				status = "Current"
			}
			c := Cluster{
				ApiServer: ep.Endpoint.Address,
				Port:      strconv.Itoa(ep.Endpoint.Port),
				Protocol:  fmt.Sprintf("%v", ep.Endpoint.ServicePort.Protocol),
				Service:   kubeEndpoints.Service,
				Status:    status,
				Locality:  kubeHttpEndpoints[indexEp].Endpoint[i].GetLocality(),
			}
			clusterList = append(clusterList, c)
		}
	}

	writer := getWriter()
	fmt.Fprintln(writer, "CLUSTER\tAPI SERVER\tPORT\tPROTOCOL\tLOCALITY\t")

	for _, cluster := range clusterList {
		fmt.Fprintf(writer, "%v\t%v\t%v\t%v\t%v\t\n",
			cluster.Status,
			cluster.ApiServer,
			cluster.Port,
			cluster.Protocol,
			cluster.Locality,
		)
	}
	writer.Flush()
	return nil
}

func getWriter() *tabwriter.Writer {
	w := tabwriter.NewWriter(os.Stdout, 5, 1, 3, ' ', 0)
	return w
}

func getMeshInfo(ns string) (*version.MeshInfo, error) {
	kubeClient, err := clientExecFactory(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	return kubeClient.GetIstioVersions(ns)
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

func getSyncInfo() (map[string][]byte, error) {
	kubeClient, err := clientExecFactory(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	return kubeClient.AllPilotsDiscoveryDo(istioNamespace,
		"GET", "/debug/syncz", nil)
}

func getCurrentClusterConfig() (*rest.Config, error) {
	kubeClient, err := kubernetes.NewClient(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}
	return kubeClient.Config, err
}
