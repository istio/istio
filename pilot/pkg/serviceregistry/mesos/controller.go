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
	"encoding/json"
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
	ISTIO_SERVICE_LABEL = "istio"
	ISTIO_MIXER_LABEL   = "istio-mixer-type"
)

var (
	DomainSuffix string
)

type MesosCall struct {
	Type string `json:"type"`
}

// ControllerOptions stores the configurable attributes of a Controller.
type ControllerOptions struct {
	// FQDN Suffix of Container ip. Default "marathon.containerip.dcos.thisdcos.directory"
	// For overlay network, IP like "9.0.x.x" will be used by dcos-net.
	ContainerDomain string
	// FQDN suffix for vip. Default ".marathon.l4lb.thisdcos.directory"
	VIPDomain string
	// FQDN suffix for agent IP. Default "marathon.agentip.dcos.thisdcos.directory"
	AgentDoamin       string
	Master            string
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

// NewController creates a new Consul controller
func NewController(options ControllerOptions) (*Controller, error) {
	log.Infof("Mesos options: %v", options)

	config := marathon.NewDefaultConfig()
	config.URL = options.Master
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
	log.Info(DomainSuffix)

	err = c.initPodmap()
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Controller) initPodmap() error {
	c.Lock()
	defer c.Unlock()
	pods, err := c.client.PodStatuses()
	if err != nil {
		return err
	}
	for _, pod := range pods {
		podInfo := getPodInfo(pod)
		if podInfo == nil {
			continue
		}
		log.Infof("ID: %v, podInfo %v", pod.ID, podInfo)
		c.podMap[pod.ID] = podInfo
	}
	return nil
}

// Services list declarations of all services in the system
func (c *Controller) Services() ([]*model.Service, error) {
	c.RLock()
	serviceMap := make(map[model.Hostname]*model.Service)
	for _, pod := range c.podMap {
		for name, _ := range pod.HostNames {
			hostname := model.Hostname(name)
			service := serviceMap[hostname]
			if service == nil {
				service = &model.Service{
					Hostname: hostname,
					// TODO: use Marathon-LB address
					Ports:        model.PortList{},
					Address:      "0.0.0.0",
					MeshExternal: false,
					Resolution:   model.ClientSideLB,
					Attributes: model.ServiceAttributes{
						Name:      string(hostname),
						Namespace: model.IstioDefaultConfigNamespace,
					},
				}
			}
			// Append only unique ports
			portExists := make(map[int]bool)
			for _, port := range service.Ports {
				portExists[port.Port] = true
			}
			for _, port := range pod.PortList {
				if !portExists[port.Port] {
					service.Ports = append(service.Ports, port)
					portExists[port.Port] = true
				}
			}
			serviceMap[hostname] = service
		}
	}
	c.RUnlock()
	log.Debugf("serviceMap: %v", serviceMap)

	out := make([]*model.Service, 0, len(serviceMap))
	for _, v := range serviceMap {
		out = append(out, v)
	}
	//arr := marshalServices(out)
	//log.Infof("Services: %v", arr)
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out, nil
}

func marshalServices(svc []*model.Service) []string {
	arr := make([]string, 0, len(svc))
	for _, v := range svc {
		j, _ := json.Marshal(*v)
		arr = append(arr, string(j))
	}
	return arr
}

func marshalServiceInstances(svc []*model.ServiceInstance) []string {
	arr := make([]string, 0, len(svc))
	for _, v := range svc {
		j, _ := json.Marshal(*v)
		arr = append(arr, string(j))
	}
	return arr
}

// GetService retrieves a service by host name if it exists
func (c *Controller) GetService(hostname model.Hostname) (*model.Service, error) {
	out := &model.Service{
		Hostname:     hostname,
		Ports:        model.PortList{},
		Address:      "0.0.0.0",
		MeshExternal: false,
		Resolution:   model.ClientSideLB,
		Attributes: model.ServiceAttributes{
			Name:      string(hostname),
			Namespace: model.IstioDefaultConfigNamespace,
		},
	}
	c.RLock()
	defer c.RUnlock()
	portExist := make(map[int]bool)
	for _, podInfo := range c.podMap {
		if podInfo.HostNames[string(hostname)] {
			for _, port := range podInfo.PortList {
				if !portExist[port.Port] {
					portExist[port.Port] = true
					out.Ports = append(out.Ports, port)
				}

			}
		}
	}

	//j, _ := json.Marshal(*out)
	//log.Infof("GetService hostname %v: %v", hostname, string(j))

	return out, nil
}

// ManagementPorts retrieves set of health check ports by instance IP.
// This does not apply to Consul service registry, as Consul does not
// manage the service instances. In future, when we integrate Nomad, we
// might revisit this function.
func (c *Controller) ManagementPorts(addr string) model.PortList {
	//log.Info("ManagementPorts not implemented")

	return nil
}

// WorkloadHealthCheckInfo retrieves set of health check info by instance IP.
// This does not apply to Consul service registry, as Consul does not
// manage the service instances. In future, when we integrate Nomad, we
// might revisit this function.
func (c *Controller) WorkloadHealthCheckInfo(addr string) model.ProbeList {
	//log.Info("WorkloadHealthCheckInfo not implemented")
	return nil
}

// InstancesByPort retrieves instances for a service that match
// any of the supplied labels. All instances match an empty tag list.
func (c *Controller) InstancesByPort(hostname model.Hostname, port int,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	name := string(hostname)
	instances := []*model.ServiceInstance{}
	c.RLock()

	//var task *TaskInstance
	arr := strings.Split(string(hostname), "_")
	if len(arr) == 2 {
		hostname = model.Hostname(arr[0])
		taskID := arr[1]
		//task = c.getTaskInstanceByID(taskID)
		log.Infof("InstancesByPort %v task: %v", hostname, taskID)
	}
	for _, pod := range c.podMap {
		if portMatch(pod.PortList, port) && pod.HostNames[name] && labels.HasSubsetOf(pod.Labels) {
			//log.Infof("port matched: %v : %v", name, port)
			instances = append(instances, getInstancesOfPod(&hostname, port, pod)...)
		}
	}
	c.RUnlock()
	//arr := marshalServiceInstances(instances)
	//log.Infof("InstancesByPort hostname: %v, port: %v, labels: %v. out: %v", hostname, port, labels, arr)

	return instances, nil
}

func (c *Controller) getTaskInstanceByID(id string) *TaskInstance {
	for _, pod := range c.podMap {
		for taskID := range pod.InstanceMap {
			if id == taskID {
				return pod.InstanceMap[taskID]
			}
		}
	}
	return nil
}

func getInstancesOfPod(hostName *model.Hostname, reqSvcPort int, pod *PodInfo) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)

	for taskID, inst := range pod.InstanceMap {
		service := model.Service{
			Hostname: *hostName,
			// TODO: use marathon-lb address
			Address:      inst.HostIP,
			Ports:        pod.PortList,
			MeshExternal: false,
			Resolution:   model.ClientSideLB,
			Attributes: model.ServiceAttributes{
				Name:      string(*hostName),
				Namespace: model.IstioDefaultConfigNamespace,
			},
		}
		for svcPort, hostPort := range inst.PortMapping {
			if svcPort == reqSvcPort {
				// hostPortInst is using hostIP:hostPort
				hostPortInst := model.ServiceInstance{
					Endpoint: model.NetworkEndpoint{
						Address: inst.HostIP,
						Port:    hostPort.Port,
						ServicePort: &model.Port{
							Name:     hostPort.Name,
							Protocol: hostPort.Protocol,
							Port:     svcPort,
						},
						UID: fmt.Sprintf("kubernetes://%s", taskID),
					},
					//AvailabilityZone: "default",
					Service: &service,
					Labels:  pod.Labels,
				}
				out = append(out, &hostPortInst)

				// containerInst is using containerIP:containerPort
				containerInst := model.ServiceInstance{
					Endpoint: model.NetworkEndpoint{
						Address: inst.IP,
						Port:    svcPort,
						ServicePort: &model.Port{
							Name:     hostPort.Name,
							Protocol: hostPort.Protocol,
							Port:     svcPort,
						},
					},
					//AvailabilityZone: "default",
					Service: &service,
					Labels:  pod.Labels,
				}
				out = append(out, &containerInst)
			}
		}
	}
	return out
}

// returns true if an instance's port matches with any in the provided list
func portMatch(portList model.PortList, servicePort int) bool {
	if servicePort == 0 {
		return true
	}
	for _, port := range portList {
		if port.Port == servicePort {
			return true
		}
	}
	return false
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
// Because hostIP is used for each instance, every host may have multiple instances of different services.
// So we use node.ID to get the matched ServiceInstances.
func (c *Controller) GetProxyServiceInstances(node *model.Proxy) ([]*model.ServiceInstance, error) {
	out := make([]*model.ServiceInstance, 0)
	//log.Infof("GetProxyServiceInstances node: %v", node)
	c.RLock()
	defer c.RUnlock()
	for _, pod := range c.podMap {
		for id, inst := range pod.InstanceMap {
			if id == node.ID {
				// serviceMap -> hostName
				log.Debugf("Find instance %v: %v", id, inst)
				out = append(out, convertTaskInstance(pod, inst)...)
			}
		}
	}

	//arr := marshalServiceInstances(out)
	//log.Infof("GetProxyServiceInstances %v: %v", node, arr)
	return out, nil
}

// Instances retrieves instances for a service and its ports that match
// any of the supplied labels. All instances match an empty tag list.
func (c *Controller) Instances(hostname model.Hostname, ports []string,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	return nil, fmt.Errorf("NOT IMPLEMENTED")
}

func convertTaskInstance(podInfo *PodInfo, inst *TaskInstance) []*model.ServiceInstance {
	out := []*model.ServiceInstance{}

	for hostName := range podInfo.HostNames {
		service := model.Service{
			Hostname:     model.Hostname(hostName),
			Address:      inst.HostIP,
			Ports:        podInfo.PortList,
			MeshExternal: false,
			Resolution:   model.ClientSideLB,
			Attributes: model.ServiceAttributes{
				Name:      hostName,
				Namespace: model.IstioDefaultConfigNamespace,
			},
		}

		// Here svcPort is used of accessing services
		// whereas hostPort is regarded as endpoints port
		for containerPort, hostPort := range inst.PortMapping {
			epPort := model.Port{
				Name: hostPort.Name,
				Port: containerPort,
				// TODO: Use other protocol
				Protocol: hostPort.Protocol,
			}
			out = append(out, &model.ServiceInstance{
				Endpoint: model.NetworkEndpoint{
					Address:     inst.IP,
					Port:        containerPort,
					ServicePort: &epPort,
				},
				//AvailabilityZone: "default",
				Service: &service,
				Labels:  podInfo.Labels,
			})

		}
	}
	return out
}

func printPodMap(podMap map[string]*PodInfo) {
	log.Info("printPodMap")
	for k := range podMap {
		info := podMap[k]
		val, _ := json.Marshal(info)
		log.Infof("pod %v: %v", k, string(val))
	}
}

/*

State Machine:

Create a Pod
deployment_info StartPod (Create item in PodMap) -> status_update_event (get inst IP and hostPort) -> deployment_step_success ScalePod, check instance number

Delete Pod
deployment_info StopPod (removePod) -> status_update_event (TASK_KILLED)

Scale Pod
deployment_info ScalePod -> status_update_event (TASK_RUNNING/TASK_KILLED)

Update Pod
deployment_info RestartPod -> status_update_event (TASK_RUNNING/TASK_KILLED)

*/
func (c *Controller) Run(stop <-chan struct{}) {
	ticker := time.NewTicker(c.syncPeriod)
	var event model.Event

	for {
		select {
		case <-ticker.C:
			c.instanceHandler(nil, event)
			c.serviceHandler(nil, event)
		case <-stop:
			log.Info("Exiting the loop")
			break
		case event := <-c.depInfoChan:
			// deployment_info event
			var depInfo *marathon.EventDeploymentInfo
			depInfo = event.Event.(*marathon.EventDeploymentInfo)
			if depInfo.CurrentStep == nil {
				log.Warnf("Illegal depInfo: %v", depInfo)
				continue
			}
			actions := depInfo.CurrentStep.Actions
			if len(actions) == 0 {
				log.Warnf("Illegal depInfo: %v", depInfo)
				continue
			} else if actions[0].Pod == "" {
				// Not a pod event, skip
				continue
			}

			podName := actions[0].Pod

			switch actions[0].Action {
			// TODO: supports graceful shutdown
			// Delete a pod from podMap
			case "StopPod":
				pod := podName
				log.Infof("Stop Pod: %v", pod)
				c.Lock()
				delete(c.podMap, pod)
				log.Infof("podMap: %v", c.podMap)
				c.Unlock()
			case "StartPod":
				// Add a new pod to podMap
				podInfo := getPodFromDepInfo(depInfo.Plan.Target.Pods, podName)
				if podInfo != nil {
					c.Lock()
					c.podMap[podName] = podInfo
					log.Infof("Add pod %v", podInfo.HostNames)
					printPodMap(c.podMap)
					c.Unlock()
				}
			case "RestartPod":
				// Update a existing pod in podMap
				podInfo := getPodFromDepInfo(depInfo.Plan.Target.Pods, podName)
				if podInfo == nil {
					continue
				}
				c.Lock()
				if c.podMap[podName] == nil {
					log.Warnf("Pod %s restarted, but no record in podMap: %v", podName, c.podMap)
					c.podMap[podName] = podInfo
				} else {
					c.podMap[podName].Labels = podInfo.Labels
					c.podMap[podName].HostNames = podInfo.HostNames
					c.podMap[podName].PortList = podInfo.PortList
					log.Infof("Update pod %v to podMap: %v", podInfo.HostNames, c.podMap)
				}
				c.Unlock()
			case "ScalePod":
				log.Infof("Scaling pod: %v", actions[0])
			}

		case event := <-c.statusUpdateChan:
			// status_update_event
			var statusUpdate *marathon.EventStatusUpdate
			statusUpdate = event.Event.(*marathon.EventStatusUpdate)

			switch statusUpdate.TaskStatus {
			case "TASK_RUNNING":
				if len(statusUpdate.Ports) == 0 {
					// No need to update a task without hostPorts
					// Like a proxy
					continue
				}
				c.addTaskToPod(statusUpdate)
			case "TASK_KILLED":
				c.deleteTaskFromPod(statusUpdate)
			}
		}
	}

	c.client.RemoveEventsListener(c.depInfoChan)
	c.client.RemoveEventsListener(c.statusUpdateChan)
}

// Trim the suffix after the last "."
// statusUpdate.TaskID: reviews.instance-34a9e7c1-1406-11e9-8921-02423471df7c.proxy
// return reviews.instance-34a9e7c1-1406-11e9-8921-02423471df7c
func getTaskID(statusUpdate *marathon.EventStatusUpdate) string {
	taskID := statusUpdate.TaskID
	lastIndex := strings.LastIndex(taskID, ".")
	if lastIndex > 0 {
		taskID = taskID[0:lastIndex]
	}
	return taskID
}

func (c *Controller) deleteTaskFromPod(statusUpdate *marathon.EventStatusUpdate) {
	taskID := getTaskID(statusUpdate)
	c.Lock()
	defer c.Unlock()
	podInfo := c.podMap[statusUpdate.AppID]
	if podInfo == nil {
		return
	}
	delete(podInfo.InstanceMap, taskID)
}

func (c *Controller) addTaskToPod(statusUpdate *marathon.EventStatusUpdate) {
	// Trim suffix
	taskID := getTaskID(statusUpdate)
	c.Lock()
	defer c.Unlock()

	podInfo := c.podMap[statusUpdate.AppID]
	if podInfo == nil {
		return
	}
	if podInfo.InstanceMap == nil {
		podInfo.InstanceMap = make(map[string]*TaskInstance)
	}
	task := TaskInstance{
		HostIP: statusUpdate.Host,
	}
	if len(statusUpdate.IPAddresses) > 0 {
		task.IP = statusUpdate.IPAddresses[0].IPAddress
	}
	podInfo.InstanceMap[taskID] = &task

	if len(podInfo.PortList) < len(statusUpdate.Ports) {
		log.Infof("Impossible case: status update: %v", statusUpdate)
		return
	}
	if task.PortMapping == nil {
		task.PortMapping = make(map[int]*model.Port)
	}
	for i, port := range statusUpdate.Ports {
		task.PortMapping[podInfo.PortList[i].Port] = &model.Port{
			Protocol: podInfo.PortList[i].Protocol,
			Name:     podInfo.PortList[i].Name,
			Port:     port,
		}
	}
	log.Infof("Added a task: %v", task)
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

// Get PodInfo based on pod spec.
// No task info for now.
func getPodFromDepInfo(pods []*marathon.Pod, podName string) *PodInfo {
	for _, pod := range pods {
		if pod.ID == podName {
			if pod.Labels[ISTIO_SERVICE_LABEL] == "" {
				log.Infof("No need to process the pod: %v", pod)
				return nil
			}
			return convertPod(pod)
		}
	}
	return nil
}

func convertPod(pod *marathon.Pod) *PodInfo {
	info := PodInfo{
		Labels: pod.Labels,
	}
	for _, con := range pod.Containers {
		// TODO: distinguish ports of different containers
		for _, ep := range con.Endpoints {
			port := model.Port{
				Name:     ep.Name,
				Protocol: convertProtocol(ep.Protocol),
				Port:     ep.ContainerPort,
			}
			if pod.Labels[ISTIO_MIXER_LABEL] != "" && strings.Contains(ep.Name, "grpc") {
				port.Protocol = model.ProtocolGRPC
				log.Infof("GPRC port: %v", ep)
			}
			info.PortList = append(info.PortList, &port)
		}
	}
	svcs := strings.Split(pod.Labels[ISTIO_SERVICE_LABEL], ",")
	hostMap := make(map[string]bool)
	for _, svc := range svcs {
		hostName := fmt.Sprintf("%s.%s", svc, DomainSuffix)
		hostMap[hostName] = true
	}
	info.HostNames = hostMap
	return &info
}

func getPodInfo(status *marathon.PodStatus) *PodInfo {
	svcNames := status.Spec.Labels[ISTIO_SERVICE_LABEL]
	if svcNames == "" {
		log.Debugf("No need to process the pod: %v", status.ID)
		return nil
	}
	svcs := strings.Split(svcNames, ",")
	hostMap := make(map[string]bool)
	for _, svc := range svcs {
		hostName := fmt.Sprintf("%s.%s", svc, DomainSuffix)
		//log.Infof("svc:%v, hostName:%v", svc, hostName)
		hostMap[hostName] = true
	}

	podInfo := &PodInfo{
		HostNames:   hostMap,
		InstanceMap: make(map[string]*TaskInstance),
		Labels:      status.Spec.Labels,
	}
	// A map of container ports
	portMap := make(map[string]*model.Port)
	for _, con := range status.Spec.Containers {
		for _, ep := range con.Endpoints {
			port := model.Port{
				Name:     ep.Name,
				Protocol: convertProtocol(ep.Protocol),
				Port:     ep.ContainerPort,
			}
			if status.Spec.Labels[ISTIO_MIXER_LABEL] != "" && strings.Contains(ep.Name, "grpc") {
				port.Protocol = model.ProtocolGRPC
				log.Infof("GPRC port: %v", ep)
			}
			portMap[ep.Name] = &port
			podInfo.PortList = append(podInfo.PortList, &port)
		}
	}

	//podInfo.ContainerPorts = portMap

	for _, inst := range status.Instances {
		taskInst := TaskInstance{
			// TODO: make sure AgentHostName is a IP!
			HostIP:      inst.AgentHostname,
			PortMapping: make(map[int]*model.Port),
		}
		// Get container IP
		for _, net := range inst.Networks {
			if len(net.Addresses) > 0 {
				taskInst.IP = net.Addresses[0]
			}
		}
		for _, con := range inst.Containers {
			for _, ep := range con.Endpoints {
				containerPort := portMap[ep.Name]
				// From container port to allocated hostPort.
				taskInst.PortMapping[containerPort.Port] = &model.Port{
					Name:     ep.Name,
					Port:     ep.AllocatedHostPort,
					Protocol: containerPort.Protocol,
				}
			}

		}
		log.Infof("New Inst:%v", taskInst)
		podInfo.InstanceMap[inst.ID] = &taskInst
	}

	return podInfo
}

func (c *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) (model.LabelsCollection, error) {
	c.RLock()
	defer c.RUnlock()
	for _, pod := range c.podMap {
		for id, _ := range pod.InstanceMap {
			if id == proxy.ID {
				return model.LabelsCollection{pod.Labels}, nil
			}
		}
	}
	return nil, nil
}

// TODO: use TCP/UDP or other protocol
func convertProtocol(protocols []string) model.Protocol {
	return model.ProtocolHTTP
}

// parseHostname extracts service name from the service hostname
func parseHostname(hostname model.Hostname) (name string, err error) {
	parts := strings.Split(string(hostname), ".")
	if len(parts) < 1 || parts[0] == "" {
		err = fmt.Errorf("missing service name from the service hostname %q", hostname)
		return
	}
	name = parts[0]
	return
}

func serviceHostname(name *string) model.Hostname {
	return model.Hostname(fmt.Sprintf("%s.%s", *name, DomainSuffix))
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

	// Need to get service account of service registered with consul
	// Currently Consul does not have service account or equivalent concept
	// As a step-1, to enabling istio security in Consul, We assume all the services run in default service account
	// This will allow all the consul services to do mTLS
	// Follow - https://goo.gl/Dt11Ct

	return []string{
		"spiffe://cluster.local/ns/default/sa/default",
	}
}
