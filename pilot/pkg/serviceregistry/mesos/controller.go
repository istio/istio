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

package mesos

import (
	"github.com/harryge00/go-marathon"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/pkg/log"
	"strings"
	"sync"
	"time"

	"fmt"
	"sort"
)

const (
	PodServiceLabel = "istio"
	PodMixerLabel   = "istio-mixer-type"

	VIPSuffix = "marathon.l4lb.thisdcos.directory"
)

var (
	DomainSuffix string
)

// ControllerOptions stores the configurable attributes of a Controller.
type ControllerOptions struct {
	// FQDN Suffix of Container ip. Default "marathon.containerip.dcos.thisdcos.directory"
	// For overlay network, IP like "9.0.x.x" will be used by dcos-net.
	ContainerDomain string
	// FQDN suffix for vip. Default ".marathon.l4lb.thisdcos.directory"
	VIPDomain string
	// FQDN suffix for agent IP. Default "marathon.agentip.dcos.thisdcos.directory"
	AgentDoamin       string
	ServerURL         string
	HTTPBasicAuthUser string
	HTTPBasicPassword string
	Interval          time.Duration
}

// Controller accepts event streams from Marathon and monitors for allocated host ports and host IP of tasks.
type Controller struct {
	sync.RWMutex
	// podMap stores podName ==> podInfo
	podMap map[string]*PodInfo

	// XDSUpdater will push EDS changes to the ADS model.
	EDSUpdater model.XDSUpdater

	syncPeriod time.Duration

	client           marathon.Marathon
	depInfoChan      marathon.EventsChannel
	statusUpdateChan marathon.EventsChannel
	serviceHandler   func(*model.Service, model.Event)
	instanceHandler  func(*model.ServiceInstance, model.Event)
}

// NewController creates a new Mesos controller
func NewController(options ControllerOptions) (*Controller, error) {
	log.Infof("Mesos options: %v", options)

	config := marathon.NewDefaultConfig()
	config.URL = options.ServerURL
	config.EventsTransport = marathon.EventsTransportSSE
	if options.HTTPBasicAuthUser != "" {
		config.HTTPBasicAuthUser = options.HTTPBasicAuthUser
		config.HTTPBasicPassword = options.HTTPBasicPassword
	}
	log.Infof("Creating a client, Marathon: %s", config.URL)

	client, err := marathon.NewClient(config)
	if err != nil {
		return nil, err
	}

	// Subscribe for Marathon's events
	depInfoChan, err := client.AddEventsListener(marathon.EventIDDeploymentInfo)
	if err != nil {
		return nil, err
	}
	statusUpdateChan, err := client.AddEventsListener(marathon.EventIDStatusUpdate)
	if err != nil {
		return nil, err
	}

	c := &Controller{
		client:           client,
		podMap:           make(map[string]*PodInfo),
		depInfoChan:      depInfoChan,
		statusUpdateChan: statusUpdateChan,
		syncPeriod:       options.Interval,
	}

	// TODO: support using other domains
	DomainSuffix = options.VIPDomain

	return c, nil
}

// Services list declarations of all services in the system
func (c *Controller) Services() ([]*model.Service, error) {
	pods, err := c.client.Pods()
	if err != nil {
		return nil, err
	}

	// Different services can have the same hostname,
	// Use Map to deduplicate
	serviceMap := make(map[model.Hostname]*model.Service)
	for _, po := range pods {
		if po.Labels[PodServiceLabel] == "" {
			continue
		}
		var portList model.PortList
		for _, con := range po.Containers {
			for _, ep := range con.Endpoints {
				port := model.Port{
					Name:     ep.Name,
					Protocol: convertProtocol(ep.Name, ep.Protocol),
					Port:     ep.ContainerPort,
				}
				portList = append(portList, &port)
			}
		}

		// Multiple services can be bound to the same pod.
		// Different service names are split by ","
		serviceNames := strings.Split(po.Labels[PodServiceLabel], ",")
		for _, svc := range serviceNames {
			hostName := serviceHostname(svc)
			service := &model.Service{
				Hostname: hostName,
				// TODO: use Marathon-LB address
				Ports:        portList,
				Address:      "0.0.0.0",
				MeshExternal: false,
				Resolution:   model.ClientSideLB,
				Attributes: model.ServiceAttributes{
					Name:      string(hostName),
					Namespace: model.IstioDefaultConfigNamespace,
				},
			}
			serviceMap[hostName] = service
		}
	}

	log.Debugf("serviceMap: %v", serviceMap)

	out := make([]*model.Service, 0, len(serviceMap))
	for _, v := range serviceMap {
		out = append(out, v)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out, nil
}

// GetService retrieves a service by host name if it exists
func (c *Controller) GetService(hostname model.Hostname) (*model.Service, error) {
	serviceName, err := parseHostname(hostname)
	if err != nil {
		return nil, err
	}
	pods, err := c.client.Pods()
	if err != nil {
		return nil, err
	}
	for _, po := range pods {
		// if hostname does not match the pod, skip it.
		if !hasService(po.Labels[PodServiceLabel], serviceName) {
			continue
		}
		portList := getPortList(&po)
		return &model.Service{
			Hostname: hostname,
			// TODO: use Marathon-LB address
			Ports:        portList,
			Address:      "0.0.0.0",
			MeshExternal: false,
			Resolution:   model.ClientSideLB,
			Attributes: model.ServiceAttributes{
				Name:      string(hostname),
				Namespace: model.IstioDefaultConfigNamespace,
			},
		}, nil

	}
	return nil, nil
}

// ManagementPorts retrieves set of health check ports by instance IP.
func (c *Controller) ManagementPorts(addr string) model.PortList {
	return nil
}

// WorkloadHealthCheckInfo retrieves set of health check info by instance IP.
func (c *Controller) WorkloadHealthCheckInfo(addr string) model.ProbeList {
	return nil
}

// InstancesByPort retrieves instances for a service that match
// any of the supplied labels. All instances match an empty tag list.
func (c *Controller) InstancesByPort(hostname model.Hostname, port int,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	name, err := parseHostname(hostname)
	if err != nil {
		return nil, err
	}

	pods, err := c.client.Pods()
	if err != nil {
		return nil, err
	}
	var instances []*model.ServiceInstance
	for _, po := range pods {
		// Match pods by labels and ports
		if po.Labels[PodServiceLabel] != name || !labels.HasSubsetOf(po.Labels) || !portMatch(&po, port) {
			continue
		}
		podStatus, err := c.client.PodStatus(po.ID)
		if err != nil {
			return nil, err
		}
		instances = append(instances, convertPodStatus(podStatus, port)...)
	}
	return instances, nil
}

// returns true if an instance's port matches with any in the provided list
func portMatch(po *marathon.Pod, port int) bool {
	for _, con := range po.Containers {
		for _, ep := range con.Endpoints {
			if ep.ContainerPort == port {
				return true
			}
		}
	}
	return false
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
// node.ID is the mesos task ID that can be used to identity the marathon pod.
// We do not use IP because we have to iterate through all pod status, which means calling marathon API multiple times.
func (c *Controller) GetProxyServiceInstances(node *model.Proxy) ([]*model.ServiceInstance, error) {
	podName, err := parseNodeID(node.ID)
	if err != nil {
		return nil, err
	}

	podStatus, err := c.client.PodStatus(podName)
	if err != nil {
		return nil, err
	}

	if podStatus.Spec == nil {
		log.Errorf("Cannot find pod status for %v", podName)
		return nil, nil
	}

	out := make([]*model.ServiceInstance, 0)
	portList := getPortList(podStatus.Spec)

	hostName := serviceHostname(podStatus.Spec.Labels[PodServiceLabel])

	// Every pod has the same containerPort, so instances' endpoints are same
	for _, inst := range podStatus.Instances {
		if len(inst.Networks) == 0 || len(inst.Networks[0].Addresses) == 0 {
			continue
		}
		addr := inst.Networks[0].Addresses[0]

		service := model.Service{
			Hostname: hostName,
			// TODO: use VIP IP as address
			Address:      addr,
			Ports:        portList,
			MeshExternal: false,
			Resolution:   model.ClientSideLB,
			Attributes: model.ServiceAttributes{
				Name:      string(hostName),
				Namespace: model.IstioDefaultConfigNamespace,
			},
		}

		for _, ep := range portList {
			out = append(out, &model.ServiceInstance{
				Endpoint: model.NetworkEndpoint{
					Address:     addr,
					Port:        ep.Port,
					ServicePort: ep,
				},
				//AvailabilityZone: "default",
				Service: &service,
				Labels:  podStatus.Spec.Labels,
			})
		}
	}

	return out, nil
}

// Instances retrieves instances for a service and its ports that match
// any of the supplied labels. All instances match an empty tag list.
func (c *Controller) Instances(hostname model.Hostname, ports []string,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	return nil, fmt.Errorf("NOT IMPLEMENTED")
}

func (c *Controller) Run(stop <-chan struct{}) {
	ticker := time.NewTicker(c.syncPeriod)
	var event model.Event
	for {
		select {
		case <-stop:
			ticker.Stop()
			return
		case <-ticker.C:
			c.instanceHandler(nil, event)
			c.serviceHandler(nil, event)
		}
	}
}

// Abstract model for Marathon pods.
type PodInfo struct {
	// List of containerPorts.
	// In Mesos, hostPort may be dynamically allocated. But containerPort is fixed.
	// So applications use hostname:containerPort to communicate with each other.
	PortList model.PortList `json:"portList"`
	// Hostname is like "app.marathon.slave.mesos". It's got from a Pod's labels.
	// A pod may have multiple hostnames
	HostNames map[string]bool `json:"hostNames"`
	// Map TaskName to its IP and portMapping.
	InstanceMap map[string]*TaskInstance `json:"instanceMap"`
	// Labels from pod specs.
	Labels map[string]string `json:"labels"`
}

// TaskInstance is abstract model for a task instance.
type TaskInstance struct {
	// IP is containerIP
	IP     string `json:"IP"`
	HostIP string `json:"hostIP"`
	// ContainerPort to HostPort mapping
	PortMapping map[int]*model.Port `json:"portMapping"`
}

func (c *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) (model.LabelsCollection, error) {
	podName, err := parseNodeID(proxy.ID)
	if err != nil {
		return nil, err
	}

	podStatus, err := c.client.PodStatus(podName)
	if err != nil {
		return nil, err
	}
	if podStatus == nil || podStatus.Spec == nil {
		return model.LabelsCollection{}, nil
	}
	return model.LabelsCollection{podStatus.Spec.Labels}, nil
}

// AppendServiceHandler implements a service catalog operation
func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	log.Info("AppendServiceHandler ")
	c.serviceHandler = f
	return nil
}

// AppendInstanceHandler implements a service catalog operation
func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	log.Info("AppendInstanceHandler ")
	c.instanceHandler = f
	return nil
}

// GetIstioServiceAccounts implements model.ServiceAccounts operation TODO
func (c *Controller) GetIstioServiceAccounts(hostname model.Hostname, ports []int) []string {

	return []string{
		"spiffe://cluster.local/ns/default/sa/default",
	}
}
