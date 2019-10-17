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

package coredatamodel

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"istio.io/api/annotation"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/pkg/log"
)

var (
	notReadyEndpointkey                        = annotation.AlphaNetworkingNotReadyEndpoints.Name
	_                   model.Controller       = &MCPDiscovery{}
	_                   model.ServiceDiscovery = &MCPDiscovery{}
)

// DiscoveryOptions stores the configurable attributes of a Control
type DiscoveryOptions struct {
	XDSUpdater   model.XDSUpdater
	Env          *model.Environment
	ClusterID    string
	DomainSuffix string
}

// mixerEnabled checks to see if mixer is enabled in the environment
// so we can set the UID on eds endpoints
func (o *DiscoveryOptions) mixerEnabled() bool {
	return o.Env != nil && o.Env.Mesh != nil && (o.Env.Mesh.MixerCheckServer != "" || o.Env.Mesh.MixerReportServer != "")
}

// MCPDiscovery provides storage for NotReadyEndpoints
type MCPDiscovery struct {
	notReadyEndpointsMu sync.RWMutex
	// [ip:port]config
	notReadyEndpoints map[string]*model.Config
	*DiscoveryOptions
}

// NewMCPDiscovery provides a new instance of Discovery
func NewMCPDiscovery(options *DiscoveryOptions) *MCPDiscovery {
	return &MCPDiscovery{
		notReadyEndpoints: make(map[string]*model.Config),
		DiscoveryOptions:  options,
	}
}

// RegisterNotReadyEndpoints registers newly received NotReadyEndpoints
// via MCP annotations
func (d *MCPDiscovery) RegisterNotReadyEndpoints(conf *model.Config) {
	d.notReadyEndpointsMu.Lock()
	defer d.notReadyEndpointsMu.Unlock()

	if nrEps, ok := conf.Annotations[notReadyEndpointkey]; ok {
		addrs := strings.Split(nrEps, ",")
		for _, addr := range addrs {
			d.notReadyEndpoints[addr] = conf
		}
	}
}

// GetProxyServiceInstances returns service instances co-located with a given proxy
func (d *MCPDiscovery) GetProxyServiceInstances(proxy *model.Proxy) ([]*model.ServiceInstance, error) {
	out := make([]*model.ServiceInstance, 0)

	// There is only one IP for kube registry
	proxyIP := proxy.IPAddresses[0]

	d.notReadyEndpointsMu.Lock()
	defer d.notReadyEndpointsMu.Unlock()
	for addr, conf := range d.notReadyEndpoints {
		notReadyIP, port, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		notReadyPort, err := strconv.Atoi(port)
		if proxyIP != notReadyIP || err != nil {
			continue
		}
		se, ok := conf.Spec.(*networking.ServiceEntry)
		if !ok {
			return nil, errors.New("getProxyServiceInstances: wrong type")
		}

		for _, h := range se.Hosts {
			for _, svcPort := range se.Ports {
				// add not ready endpoint
				out = append(out, d.notReadyServiceInstance(se, svcPort, conf, h, notReadyIP, notReadyPort))
				// add other endpoints
				out = append(out, d.serviceInstances(se, svcPort, conf, h)...)
			}
		}
	}
	return out, nil
}

// GetService implements a service catalog operation by hostname specified.
func (d *MCPDiscovery) GetService(hostname host.Name) (*model.Service, error) {
	var uid string
	d.notReadyEndpointsMu.Lock()
	defer d.notReadyEndpointsMu.Unlock()
	for _, conf := range d.notReadyEndpoints {
		h := kube.ServiceHostname(conf.Name, conf.Namespace, d.DomainSuffix)
		if hostname == h {
			se, ok := conf.Spec.(*networking.ServiceEntry)
			if !ok {
				return nil, errors.New("getSerivce: wrong type")
			}
			if d.mixerEnabled() {
				uid = fmt.Sprintf("kubernetes://%s.%s", conf.Name, conf.Namespace)
			}

			return &model.Service{
				//TODO (Nino-K): serviceAccount, Address?
				Hostname:   h,
				Ports:      convertServicePorts(se.Ports),
				Resolution: model.Resolution(int(se.Resolution)),
				Attributes: model.ServiceAttributes{
					Name:      conf.Name,
					Namespace: conf.Namespace,
					UID:       uid,
				},
			}, nil
		}
	}
	return nil, errors.New("getSerivce: service not found")
}

// InstancesByPort implements a service catalog operation
func (d *MCPDiscovery) InstancesByPort(svc *model.Service, servicePort int, labels labels.Collection) ([]*model.ServiceInstance, error) {
	out := make([]*model.ServiceInstance, 0)

	d.notReadyEndpointsMu.Lock()
	defer d.notReadyEndpointsMu.Unlock()
	for addr, conf := range d.notReadyEndpoints {
		notReadyIP, port, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		notReadyPort, err := strconv.Atoi(port)
		if err != nil {
			continue
		}
		se, ok := conf.Spec.(*networking.ServiceEntry)
		if !ok {
			return nil, errors.New("instancesByPort: wrong type")
		}

		for _, svcPort := range se.Ports {
			for _, h := range se.Hosts {
				if uint32(servicePort) == svcPort.Number {
					// add not ready endpoint
					out = append(out, d.notReadyServiceInstance(se, svcPort, conf, h, notReadyIP, notReadyPort))
					// add other endpoints
					out = append(out, d.serviceInstances(se, svcPort, conf, h)...)

				}
			}
		}
	}
	return out, nil
}

func (d *MCPDiscovery) notReadyServiceInstance(
	se *networking.ServiceEntry,
	svcPort *networking.Port,
	conf *model.Config, hostname,
	ip string,
	port int) *model.ServiceInstance {
	var uid string
	if d.mixerEnabled() {
		uid = fmt.Sprintf("kubernetes://%s.%s", conf.Name, conf.Namespace)
	}
	out := &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			UID:     uid,
			Address: ip,
			Port:    port,
			ServicePort: &model.Port{
				Name:     svcPort.Name,
				Port:     int(svcPort.Number),
				Protocol: protocol.Instance(svcPort.Protocol),
			},
		},
		// ServiceAccount is retrieved in Kube/Controller
		// using IdentityPodAnnotation
		//ServiceAccount and Address: ???, //TODO: nino-k
		Service: &model.Service{
			Hostname:   host.Name(hostname),
			Ports:      convertServicePorts(se.Ports),
			Resolution: model.Resolution(int(se.Resolution)),
			Attributes: model.ServiceAttributes{
				Name:      conf.Name,
				Namespace: conf.Namespace,
				UID:       uid,
			},
		},
		Labels: labels.Instance(conf.Labels),
	}
	return out
}

func (d *MCPDiscovery) serviceInstances(
	se *networking.ServiceEntry,
	svcPort *networking.Port,
	conf *model.Config,
	hostname string) (out []*model.ServiceInstance) {
	var uid string
	if d.mixerEnabled() {
		uid = fmt.Sprintf("kubernetes://%s.%s", conf.Name, conf.Namespace)
	}
	for _, ep := range se.Endpoints {
		for _, epPort := range ep.Ports {
			si := &model.ServiceInstance{
				Endpoint: model.NetworkEndpoint{
					UID:      uid,
					Address:  ep.Address,
					Port:     int(epPort),
					Locality: ep.Locality,
					LbWeight: ep.Weight,
					Network:  ep.Network,
					ServicePort: &model.Port{
						Name:     svcPort.Name,
						Port:     int(svcPort.Number),
						Protocol: protocol.Instance(svcPort.Protocol),
					},
				},
				// ServiceAccount is retrieved in Kube/Controller
				// using IdentityPodAnnotation
				//ServiceAccount, Address: ???, //TODO: nino-k
				Service: &model.Service{
					Hostname:   host.Name(hostname),
					Ports:      convertServicePorts(se.Ports),
					Resolution: model.Resolution(int(se.Resolution)),
					Attributes: model.ServiceAttributes{
						Name:      conf.Name,
						Namespace: conf.Namespace,
						UID:       uid,
					},
				},
				Labels: labels.Instance(conf.Labels),
			}
			out = append(out, si)
		}
	}
	return out
}

func convertServicePorts(ports []*networking.Port) model.PortList {
	out := make(model.PortList, 0)
	for _, port := range ports {
		out = append(out, &model.Port{
			Name:     port.Name,
			Port:     int(port.Number),
			Protocol: protocol.Instance(port.Protocol),
		})
	}

	return out
}

// Services Not Supported
func (d *MCPDiscovery) Services() ([]*model.Service, error) {
	log.Warnf("Services %s", errUnsupported)
	return nil, nil
}

// ManagementPorts Not Supported
func (d *MCPDiscovery) ManagementPorts(addr string) model.PortList {
	log.Warnf("ManagementPorts %s", errUnsupported)
	return nil
}

// WorkloadHealthCheckInfo Not Supported
func (d *MCPDiscovery) WorkloadHealthCheckInfo(addr string) model.ProbeList {
	log.Warnf("WorkloadHealthCheckInfo %s", errUnsupported)
	return nil
}

// GetIstioServiceAccounts Not Supported
func (d *MCPDiscovery) GetIstioServiceAccounts(svc *model.Service, ports []int) []string {
	log.Warnf("GetIstioServiceAccounts %s", errUnsupported)
	return nil
}

// GetProxyWorkloadLabels Not Supported
func (d *MCPDiscovery) GetProxyWorkloadLabels(*model.Proxy) (labels.Collection, error) {
	log.Warnf("GetProxyWorkloadLabels %s", errUnsupported)
	return nil, nil
}

// model Controller

// AppendServiceHandler Not Supported
func (d *MCPDiscovery) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	log.Warnf("AppendServiceHandler %s", errUnsupported)
	return nil
}

// AppendInstanceHandler Not Supported
func (d *MCPDiscovery) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	log.Warnf("AppendInstanceHandler %s", errUnsupported)
	return nil
}

// Run until a signal is received
func (d *MCPDiscovery) Run(stop <-chan struct{}) {
	log.Warnf("Run %s", errUnsupported)
}
