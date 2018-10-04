package model

import (
	"fmt"
	"sync"
	"errors"
)

// NewMemServiceDiscovery builds an in-memory MemServiceDiscovery
func NewMemServiceDiscovery(services map[Hostname]*Service, versions int) *MemServiceDiscovery {
	return &MemServiceDiscovery{
		services:            services,
		versions:            versions,
		Controller:          &memServiceController{},
		instancesByPortNum:  map[string][]*ServiceInstance{},
		instancesByPortName: map[string][]*ServiceInstance{},
		ip2instance:         map[string][]*ServiceInstance{},
	}
}

// TODO: the mock was used for test setup, has no mutex. This will also be used for
// integration and load tests, will need to add mutex as we cleanup the code.

type memServiceController struct {
	svcHandlers  []func(*Service, Event)
	instHandlers []func(*ServiceInstance, Event)
}

func (c *memServiceController) AppendServiceHandler(f func(*Service, Event)) error {
	c.svcHandlers = append(c.svcHandlers, f)
	return nil
}

func (c *memServiceController) AppendInstanceHandler(f func(*ServiceInstance, Event)) error {
	c.instHandlers = append(c.instHandlers, f)
	return nil
}

func (c *memServiceController) Run(<-chan struct{}) {}

// MemServiceDiscovery is a mock discovery interface
type MemServiceDiscovery struct {
	services map[Hostname]*Service
	// Endpoints table. Key is the fqdn of the service, ':', port
	instancesByPortNum            map[string][]*ServiceInstance
	instancesByPortName           map[string][]*ServiceInstance
	ip2instance                   map[string][]*ServiceInstance
	versions                      int
	WantGetProxyServiceInstances  []*ServiceInstance
	ServicesError                 error
	GetServiceError               error
	InstancesError                error
	GetProxyServiceInstancesError error
	Controller                    Controller

	// Single mutex for now - it's for debug only.
	mutex sync.Mutex
}

// ClearErrors clear errors used for mocking failures during MemServiceDiscovery interface methods
func (sd *MemServiceDiscovery) ClearErrors() {
	sd.ServicesError = nil
	sd.GetServiceError = nil
	sd.InstancesError = nil
	sd.GetProxyServiceInstancesError = nil
}

// AddService adds an in-memory service.
func (sd *MemServiceDiscovery) AddService(name Hostname, svc *Service) {
	sd.mutex.Lock()
	sd.services[name] = svc
	sd.mutex.Unlock()
	// TODO: notify listeners
}

// AddInstance adds an in-memory instance.
func (sd *MemServiceDiscovery) AddInstance(service Hostname, instance *ServiceInstance) {
	// WIP: add enough code to allow tests and load tests to work
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	svc := sd.services[service]
	if svc == nil {
		return
	}
	instance.Service = svc
	sd.ip2instance[instance.Endpoint.Address] = []*ServiceInstance{instance}

	key := fmt.Sprintf("%s:%d", service, instance.Endpoint.ServicePort.Port)
	instanceList := sd.instancesByPortNum[key]
	sd.instancesByPortNum[key] = append(instanceList, instance)

	key = fmt.Sprintf("%s:%s", service, instance.Endpoint.ServicePort.Name)
	instanceList = sd.instancesByPortName[key]
	sd.instancesByPortName[key] = append(instanceList, instance)
}

// AddEndpoint adds an endpoint to a service.
func (sd *MemServiceDiscovery) AddEndpoint(service Hostname, servicePortName string, servicePort int, address string, port int, serviceAccount string) *ServiceInstance {
	instance := &ServiceInstance{
		Endpoint: NetworkEndpoint{
			Address: address,
			Port:    port,
			ServicePort: &Port{
				Name:     servicePortName,
				Port:     servicePort,
				Protocol: ProtocolHTTP,
			},
		},
		ServiceAccount: serviceAccount,
	}
	sd.AddInstance(service, instance)
	return instance
}

// Services implements discovery interface
// Each call to Services() should return a list of new *Service
func (sd *MemServiceDiscovery) Services() ([]*Service, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.ServicesError != nil {
		return nil, sd.ServicesError
	}
	out := make([]*Service, 0, len(sd.services))
	for _, service := range sd.services {
		// Make a new service out of the existing one
		newSvc := *service
		out = append(out, &newSvc)
	}
	return out, sd.ServicesError
}

// GetService implements discovery interface
// Each call to GetService() should return a new *Service
func (sd *MemServiceDiscovery) GetService(hostname Hostname) (*Service, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.GetServiceError != nil {
		return nil, sd.GetServiceError
	}
	val := sd.services[hostname]
	if val == nil {
		return nil, errors.New("missing service")
	}
	// Make a new service out of the existing one
	newSvc := *val
	return &newSvc, sd.GetServiceError
}

// InstancesByPort filters the service instances by labels. This assumes single port, as is
// used by EDS/ADS.
func (sd *MemServiceDiscovery) InstancesByPort(hostname Hostname, port int,
		labels LabelsCollection) ([]*ServiceInstance, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.InstancesError != nil {
		return nil, sd.InstancesError
	}
	key := fmt.Sprintf("%s:%d", string(hostname), port)
	instances, ok := sd.instancesByPortNum[key]
	if !ok {
		return nil, nil
	}
	return instances, nil
}

// GetProxyServiceInstances returns service instances associated with a node, resulting in
// 'in' services.
func (sd *MemServiceDiscovery) GetProxyServiceInstances(node *Proxy) ([]*ServiceInstance, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.GetProxyServiceInstancesError != nil {
		return nil, sd.GetProxyServiceInstancesError
	}
	if sd.WantGetProxyServiceInstances != nil {
		return sd.WantGetProxyServiceInstances, nil
	}
	out := make([]*ServiceInstance, 0)
	si, found := sd.ip2instance[node.IPAddress]
	if found {
		out = append(out, si...)
	}
	return out, sd.GetProxyServiceInstancesError
}

// ManagementPorts implements discovery interface
func (sd *MemServiceDiscovery) ManagementPorts(addr string) PortList {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	return PortList{{
		Name:     "http",
		Port:     3333,
		Protocol: ProtocolHTTP,
	}, {
		Name:     "custom",
		Port:     9999,
		Protocol: ProtocolTCP,
	}}
}

// WorkloadHealthCheckInfo implements discovery interface
func (sd *MemServiceDiscovery) WorkloadHealthCheckInfo(addr string) ProbeList {
	return nil
}

// GetIstioServiceAccounts gets the Istio service accounts for a service hostname.
// TODO(incfly): delete this once PR is in removing the requirements of having this function.
func (sd *MemServiceDiscovery) GetIstioServiceAccounts(hostname Hostname, ports []int) []string {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if hostname == "world.default.svc.cluster.local" {
		return []string{
			"spiffe://cluster.local/ns/default/sa/serviceaccount1",
			"spiffe://cluster.local/ns/default/sa/serviceaccount2",
		}
	}
	return make([]string, 0)
}