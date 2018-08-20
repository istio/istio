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

package cloudfoundry

import (
	"fmt"

	copilotapi "code.cloudfoundry.org/copilot/api"

	"istio.io/istio/pilot/pkg/model"
)

const (
	cfLabel = "cfapp"
)

//go:generate $GOPATH/src/istio.io/istio/bin/counterfeiter.sh -o ./fakes/route_cacher.go --fake-name RouteCacher . routeCacher
type routeCacher interface {
	Get() (*copilotapi.RoutesResponse, error)
	GetInternal() (*copilotapi.InternalRoutesResponse, error)
}

// ServiceDiscovery implements the model.ServiceDiscovery interface for Cloud Foundry
type ServiceDiscovery struct {
	RoutesRepo routeCacher

	// Cloud Foundry currently only supports applications exposing a single HTTP or TCP port
	// It is typically 8080
	ServicePort int
}

// Services implements a service catalog operation
func (sd *ServiceDiscovery) Services() ([]*model.Service, error) {
	resp, err := sd.RoutesRepo.Get()
	if err != nil {
		return nil, fmt.Errorf("getting services: %s", err)
	}
	services := make([]*model.Service, 0, len(resp.GetRoutes()))

	port := sd.servicePort()
	for _, route := range resp.GetRoutes() {
		hostname := model.Hostname(route.Hostname)
		services = append(services, &model.Service{
			Hostname:     hostname,
			Ports:        []*model.Port{port},
			MeshExternal: false,
			Resolution:   model.ClientSideLB,
			Attributes: model.ServiceAttributes{
				Name:      string(hostname),
				Namespace: model.IstioDefaultConfigNamespace,
			},
		})
	}

	internalRoutesResp, err := sd.RoutesRepo.GetInternal()
	if err != nil {
		return nil, fmt.Errorf("getting services: %s", err)
	}

	internalRouteServicePort := &model.Port{
		Port:     sd.ServicePort,
		Protocol: model.ProtocolTCP,
		Name:     "tcp",
	}

	for _, internalRoute := range internalRoutesResp.GetInternalRoutes() {
		hostname := model.Hostname((internalRoute.Hostname))
		services = append(services, &model.Service{
			Hostname:     hostname,
			Address:      internalRoute.Vip,
			Ports:        []*model.Port{internalRouteServicePort},
			MeshExternal: false,
			Resolution:   model.ClientSideLB,
			Attributes: model.ServiceAttributes{
				Name:      string(hostname),
				Namespace: model.IstioDefaultConfigNamespace,
			},
		})
	}

	return services, nil
}

// GetService implements a service catalog operation
func (sd *ServiceDiscovery) GetService(hostname model.Hostname) (*model.Service, error) {
	services, err := sd.Services()
	if err != nil {
		return nil, err
	}
	for _, svc := range services {
		if svc.Hostname == hostname {
			return svc, nil
		}
	}
	return nil, nil
}

// InstancesByPort implements a service catalog operation
func (sd *ServiceDiscovery) InstancesByPort(hostname model.Hostname, _ int, labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	resp, err := sd.RoutesRepo.Get()
	if err != nil {
		return nil, fmt.Errorf("getting routes: %s", err)
	}

	instances := make([]*model.ServiceInstance, 0)
	var matchedRoutes []*copilotapi.RouteWithBackends
	for _, route := range resp.GetRoutes() {
		if route.Hostname == string(hostname) {
			matchedRoutes = append(matchedRoutes, route)
		}
	}

	for _, matchedRoute := range matchedRoutes {
		backends := matchedRoute.GetBackends()
		if backends == nil {
			continue
		}

		for _, backend := range backends.GetBackends() {
			port := sd.servicePort()

			inst := &model.ServiceInstance{
				Endpoint: model.NetworkEndpoint{
					Address:     backend.Address,
					Port:        int(backend.Port),
					ServicePort: port,
				},
				Service: &model.Service{
					Hostname:     hostname,
					Ports:        []*model.Port{port},
					MeshExternal: false,
					Resolution:   model.ClientSideLB,
					Attributes: model.ServiceAttributes{
						Name:      string(hostname),
						Namespace: model.IstioDefaultConfigNamespace,
					},
				},
			}

			inst.Labels = model.Labels{cfLabel: matchedRoute.GetCapiProcessGuid()}

			for _, label := range labels {
				if v, ok := label[cfLabel]; ok {
					if v == matchedRoute.GetCapiProcessGuid() {
						instances = append(instances, inst)
					}
				}
			}
		}
	}

	internalRoutesResp, err := sd.RoutesRepo.GetInternal()
	if err != nil {
		return nil, fmt.Errorf("getting internal routes: %s", err)
	}

	internalRouteServicePort := &model.Port{
		Port:     sd.ServicePort,
		Protocol: model.ProtocolTCP,
		Name:     "tcp",
	}

	for _, internalRoute := range internalRoutesResp.GetInternalRoutes() {
		for _, backend := range internalRoute.GetBackends().Backends {
			if internalRoute.Hostname == string(hostname) {
				instances = append(instances, &model.ServiceInstance{
					Endpoint: model.NetworkEndpoint{
						Address:     backend.Address,
						Port:        int(backend.Port),
						ServicePort: internalRouteServicePort,
					},
					Service: &model.Service{
						Hostname:     hostname,
						Address:      internalRoute.Vip,
						Ports:        []*model.Port{internalRouteServicePort},
						MeshExternal: false,
						Resolution:   model.ClientSideLB,
						Attributes: model.ServiceAttributes{
							Name:      string(hostname),
							Namespace: model.IstioDefaultConfigNamespace,
						},
					},
				})
			}
		}
	}

	return instances, nil
}

// GetProxyServiceInstances returns all service instances running on a particular proxy
// Cloud Foundry integration is currently ingress-only -- there is no sidecar support yet.
// So this function always returns an empty slice.
func (sd *ServiceDiscovery) GetProxyServiceInstances(proxy *model.Proxy) ([]*model.ServiceInstance, error) {
	return nil, nil
}

// ManagementPorts is not currently implemented for Cloud Foundry
func (sd *ServiceDiscovery) ManagementPorts(addr string) model.PortList {
	return nil
}

// WorkloadHealthCheckInfo is not currently implemented for Cloud Foundry
func (sd *ServiceDiscovery) WorkloadHealthCheckInfo(addr string) model.ProbeList {
	return nil
}

// all CF apps listen on the same port (for now)
func (sd *ServiceDiscovery) servicePort() *model.Port {
	return &model.Port{
		Port:     sd.ServicePort,
		Protocol: model.ProtocolHTTP,
		Name:     "http",
	}
}
