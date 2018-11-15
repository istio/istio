// Copyright 2018 Istio Authors
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

package genbinding

import (
        "errors"
	"fmt"
	"hash/fnv"
	"net"
        "regexp"
	"strconv"
	"strings"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

type hostPort struct {
	host string
	port int
}

const defaultMultiMeshPort = "15443"

var ValidHostnameRegex = regexp.MustCompile(`^(([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])\.)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9])$`)

// CreateBinding converts desired multicluster state into Istio state
func CreateBinding(service string, clusters []string, labels map[string]string, egressGateway string, namespace string) ([]model.Config, error) { // nolint: lll

	remoteService, err := parseNet(service, "")
	if err != nil {
		return nil, err
	}

	var remoteClusters []hostPort
	for _, cluster := range clusters {
		remoteCluster, err := parseNet(cluster, defaultMultiMeshPort)
		if err != nil {
			return nil, err
		}
		remoteClusters = append(remoteClusters, *remoteCluster)
	}

	istioConfig, err := serviceToServiceEntrySniCluster(*remoteService, remoteClusters, labels, egressGateway, namespace)
	return istioConfig, err
}

// serviceToServiceEntry() creates a ServiceEntry pointing to istio-egressgateway
func serviceToServiceEntrySniCluster(remoteService hostPort, remoteClusters []hostPort, labels map[string]string, egressGateway string, namespace string) ([]model.Config, error) { // nolint: lll
	protocol := "http"
        var egresslabels map[string]string
	// It is strongly recommended addresses for different hosts don't clash;
	// we use the hash to make clashing unlikely.
	h := fnv.New32a()
	h.Write([]byte(remoteService.host))
	address := fmt.Sprintf("127.255.%d.%d", h.Sum32()&255, (h.Sum32()>>8)&255)

	serviceEntry := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.ServiceEntry.Type,
			Group:     model.ServiceEntry.Group + model.IstioAPIGroupDomain,
			Version:   model.ServiceEntry.Version,
			Name:      fmt.Sprintf("service-entry-%s", remoteService.host),
			Namespace: namespace,
			// Annotations: annotations(config), //  TODO (MB)
		},
		Spec: &v1alpha3.ServiceEntry{
			Hosts: []string{remoteService.host},
			Ports: []*v1alpha3.Port{
				&v1alpha3.Port{
					Number:   uint32(remoteService.port),
					Protocol: "HTTP",
					Name:     "http",
				},
			},
			Addresses:  []string{address},
			Location:   v1alpha3.ServiceEntry_MESH_INTERNAL,
			Resolution: v1alpha3.ServiceEntry_DNS,
			Endpoints:  []*v1alpha3.ServiceEntry_Endpoint{},
		},
	}

	egresslabels = make(map[string]string)
        egresslabels["network"] = "external"
	spec := serviceEntry.Spec.(*v1alpha3.ServiceEntry)
	for _, cluster := range remoteClusters {
		endpoint := &v1alpha3.ServiceEntry_Endpoint{
			Address: cluster.host,
			Ports:   make(map[string]uint32),
		}
		endpoint.Ports[protocol] = uint32(cluster.port)
		if len(labels) > 0 {
		   	if egressGateway != "" {
			   	labels["network"] = "external"
			}
			endpoint.Labels = labels
		} else if egressGateway != "" {
			endpoint.Labels = egresslabels
                }
		spec.Endpoints = append(spec.Endpoints, endpoint)
	}

        if egressGateway != "" {
               egressGatewayEntry, err := parseNet(egressGateway, defaultMultiMeshPort)
	       if err != nil {
                        return nil, err
               }
                endpoint := &v1alpha3.ServiceEntry_Endpoint{
                        Address: egressGatewayEntry.host,
			Ports:   make(map[string]uint32),
                }
                endpoint.Ports[protocol] = uint32(egressGatewayEntry.port)
                spec.Endpoints = append(spec.Endpoints, endpoint)
        }

	return []model.Config{serviceEntry}, nil
}

func parseNet(s, defaultPort string) (*hostPort, error) {
	var host, port string
	var err error

	if defaultPort != "" && !strings.Contains(s, ":") {
		host, port, err= s, defaultPort, nil
	} else {
	        host, port, err = net.SplitHostPort(s)
	}
	if err != nil {
		return nil, err
	}

	i, err := strconv.Atoi(port)
	if err != nil {
		return nil, err
	}
	hostIP := net.ParseIP(host)
	if hostIP == nil {
		if ValidHostnameRegex.MatchString(host) == false {
			return nil, errors.New("Invalid Name or IP address")
		}
	}
	return &hostPort{host, i}, nil
}
