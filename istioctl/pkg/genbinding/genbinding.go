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
	"fmt"
	"hash/fnv"
	"net"
	"strconv"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

type hostPort struct {
	host string
	port int
}

// CreateBinding converts desired multicluster state into Istio state
func CreateBinding(service string, clusters []string, labels map[string]string, namespace string) ([]model.Config, error) {

	host, port, err := net.SplitHostPort(service)
	if err != nil {
		return nil, err
	}
	i, err := strconv.Atoi(port)
	if err != nil {
		return nil, err
	}
	remoteService := hostPort{host, i}

	var remoteClusters []hostPort
	for _, cluster := range clusters {
		host, port, err := net.SplitHostPort(cluster)
		if err != nil {
			return nil, err
		}
		i, err := strconv.Atoi(port)
		if err != nil {
			return nil, err
		}
		remoteClusters = append(remoteClusters, hostPort{host, i})
	}

	istioConfig, err := serviceToServiceEntrySniCluster(remoteService, remoteClusters, labels, namespace)
	return istioConfig, err
}

// serviceToServiceEntry() creates a ServiceEntry pointing to istio-egressgateway
func serviceToServiceEntrySniCluster(remoteService hostPort, remoteClusters []hostPort, labels map[string]string, namespace string) ([]model.Config, error) { // nolint: lll
	protocol := "http"

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

	spec := serviceEntry.Spec.(*v1alpha3.ServiceEntry)
	for _, cluster := range remoteClusters {
		endpoint := &v1alpha3.ServiceEntry_Endpoint{
			Address: cluster.host,
			Ports:   make(map[string]uint32),
		}
		endpoint.Ports[protocol] = uint32(cluster.port)
		if len(labels) > 0 {
			endpoint.Labels = labels
		}
		spec.Endpoints = append(spec.Endpoints, endpoint)
	}

	return []model.Config{serviceEntry}, nil
}
