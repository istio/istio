/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package zookeeper

import (
	"github.com/pkg/errors"
	"github.com/samuel/go-zookeeper/zk"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"strings"
	"time"
)

type serviceHandler func(*model.Service, model.Event)
type instanceHandler func(*model.ServiceInstance, model.Event)

// Controller communicate with zookeeper.
type Controller struct {
	client           *Client
	serviceHandlers  []serviceHandler
	instanceHandlers []instanceHandler
}

// NewController create a Controller instance
func NewController(address string, root string) (*Controller, error) {
	servers := strings.Split(address, ",")
	conn, _, err := zk.Connect(servers, 15*time.Second)
	if err != nil {
		return nil, err
	}
	client := NewClient("/sofa-rpc", conn)
	controller := &Controller{
		client: client,
	}
	return controller, nil
}

// AppendServiceHandler notifies about changes to the service catalog.
func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.serviceHandlers = append(c.serviceHandlers, f)
	return nil
}

// AppendInstanceHandler notifies about changes to the service instances
// for a service.
func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.instanceHandlers = append(c.instanceHandlers, f)
	return nil
}

// Run until a signal is received
func (c *Controller) Run(stop <-chan struct{}) {
	if err := c.client.Start(); err != nil {
		log.Warnf("Can not connect to zk %s", c.client)
		return
	}

	for {

		select {
		case event := <-c.client.Events():
			switch event.EventType {
			case ServiceAdded:
				log.Infof("Service %s added", event.Service)
				service := toService(event.Service)
				for _, handler := range c.serviceHandlers {
					go handler(service, model.EventAdd)
				}
			case ServiceDeleted:
				log.Infof("Service %s deleted", event.Service)
				service := toService(event.Service)
				for _, handler := range c.serviceHandlers {
					go handler(service, model.EventDelete)
				}
			case ServiceInstanceAdded:
				log.Infof("Service instance %v added", event.Instance)
				instance, err := toInstance(event.Instance)
				if err != nil {
					break
				}
				for _, handler := range c.instanceHandlers {
					go handler(instance, model.EventAdd)
				}
			case ServiceInstanceDeleted:
				log.Infof("Service instance %v deleted", event.Instance)
				instance, err := toInstance(event.Instance)
				if err != nil {
					break
				}
				for _, handler := range c.instanceHandlers {
					go handler(instance, model.EventDelete)
				}
			}
		case <-stop:
			c.client.Stop()
			// also close zk
		}
	}
}

// Services list all service within zookeeper registry
func (c *Controller) Services() ([]*model.Service, error) {
	services := c.client.Services()
	result := make([]*model.Service, 0, len(services))
	for _, service := range services {
		result = append(result, toService(service))
	}
	return result, nil
}

// GetService retrieve dedicated service with specific host name
func (c *Controller) GetService(hostname model.Hostname) (*model.Service, error) {
	s := c.client.Service(string(hostname))
	if s == nil {
		return nil, errors.Errorf("service %s not exist", hostname)
	}
	return toService(s), nil
}

// GetServiceAttributes implements a service catalog operation.
func (sd *Controller) GetServiceAttributes(hostname model.Hostname) (*model.ServiceAttributes, error) {
	svc, err := sd.GetService(hostname)
	if svc != nil {
		return &model.ServiceAttributes{
			Name:      hostname.String(),
			Namespace: model.IstioDefaultConfigNamespace}, nil
	}
	return nil, err
}

// WorkloadHealthCheckInfo retrieves set of health check info by instance IP.
// This will be implemented later
func (c *Controller) WorkloadHealthCheckInfo(addr string) model.ProbeList {
	return nil
}

// Instances list all instance for a specific host name
func (c *Controller) Instances(hostname model.Hostname, ports []string,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	instances := c.client.Instances(string(hostname))
	result := make([]*model.ServiceInstance, 0)
	for _, instance := range instances {
		i, err := toInstance(instance)
		if err != nil {
			continue
		}
		for _, name := range ports {
			if name == instance.Port.Protocol && labels.HasSubsetOf(i.Labels) {
				result = append(result, i)
			}
		}
	}
	return result, nil
}

// Instances list all instance for a specific host name
func (c *Controller) InstancesByPort(hostname model.Hostname, port int,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	instances := c.client.Instances(string(hostname))
	result := make([]*model.ServiceInstance, 0)
	for _, instance := range instances {
		i, err := toInstance(instance)
		if err != nil {
			continue
		}
		if labels.HasSubsetOf(i.Labels) && portMatch(i, port) {
			result = append(result, i)
		}
	}
	return result, nil
}

func (c *Controller) GetProxyServiceInstances(proxy *model.Proxy) ([]*model.ServiceInstance, error) {
	instances := c.client.InstancesByHost(proxy.IPAddress)
	result := make([]*model.ServiceInstance, 0, len(instances))
	for _, instance := range instances {
		i, err := toInstance(instance)
		if err == nil {
			result = append(result, i)
		}
	}
	return result, nil
}

func (c *Controller) ManagementPorts(addr string) model.PortList {
	return nil
}

// GetIstioServiceAccounts implements model.ServiceAccounts operation TODO
func (c *Controller) GetIstioServiceAccounts(hostname model.Hostname, ports []string) []string {
	return []string{
		"spiffe://cluster.local/ns/default/sa/default",
	}
}

// returns true if an instance's port matches with any in the provided list
func portMatch(instance *model.ServiceInstance, port int) bool {
	return port == 0 || port == instance.Endpoint.ServicePort.Port
}

func toService(s *Service) *model.Service {
	ports := make([]*model.Port, 0, len(s.Ports()))
	for _, p := range s.Ports() {
		port := toPort(p)
		ports = append(ports, port)
	}
	service := &model.Service{
		Hostname:   model.Hostname(s.Hostname()),
		Resolution: model.ClientSideLB,
		Ports:      ports,
	}
	return service
}

// The endpoint in sofa rpc registry looks like bolt://192.168.1.100:22000?xxx=yyy
func toInstance(instance *Instance) (*model.ServiceInstance, error) {
	networkEndpoint := model.NetworkEndpoint{
		Family:      model.AddressFamilyTCP,
		Address:     instance.Host,
		Port:        instance.Port.Portoi(),
		ServicePort: toPort(instance.Port),
	}

	return &model.ServiceInstance{
		Endpoint: networkEndpoint,
		Service:  toService(instance.Service),
		Labels:   instance.Labels,
	}, nil
}

func toPort(port *Port) *model.Port {
	return &model.Port{
		Name:     port.Protocol,
		Protocol: model.ParseProtocol(port.Protocol),
		Port:     port.Portoi(),
	}
}
