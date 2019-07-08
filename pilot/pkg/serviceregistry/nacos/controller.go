package nacos

import (
	nacos_model "github.com/nacos-group/nacos-sdk-go/model"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/spiffe"
	"istio.io/pkg/log"
	"reflect"
	"time"
)

type serviceHandler func(*model.Service, model.Event)
type instanceHandler func(*model.ServiceInstance, model.Event)

// Controller communicates with Consul and monitors for changes
type Controller struct {
	client           *Client
	serviceHandlers  []serviceHandler
	instanceHandlers []instanceHandler
	duration         time.Duration
}

// NewController creates a new Nacos controller
func NewController(addr string, duration time.Duration) (*Controller, error) {
	client, err := NewClient(addr)
	return &Controller{
		client:           client,
		serviceHandlers:  make([]serviceHandler, 0),
		instanceHandlers: make([]instanceHandler, 0),
		duration:         duration,
	}, err
}

func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.serviceHandlers = append(c.serviceHandlers, f)
	return nil
}

func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.instanceHandlers = append(c.instanceHandlers, f)
	return nil
}

//==================ServiceDiscovery Interface //
// Services() ([]*Service, error)
// GetService(hostname Hostname) (*Service, error)
// InstancesByPort(hostname Hostname, servicePort int, labels LabelsCollection) ([]*ServiceInstance, error)
// GetProxyServiceInstances(*Proxy) ([]*ServiceInstance, error)
// GetProxyWorkloadLabels(*Proxy) (LabelsCollection, error)
// ManagementPorts(addr string) PortList
// WorkloadHealthCheckInfo(addr string) ProbeList
// GetIstioServiceAccounts(hostname Hostname, ports []int) []string

// 获取所有的Service信息
func (c *Controller) Services() ([]*model.Service, error) {
	servicesInfo, err := c.client.getAllServices()
	if err != nil {
		return nil, err
	}
	result := make([]*model.Service, 0, len(servicesInfo))
	for _, service := range servicesInfo {
		result = append(result, convertService(service))
	}
	return nil, nil
}

// GetService retrieves a service by host name if it exists
// 根据hostName查询对应的Service是否存在
func (c *Controller) GetService(hostname model.Hostname) (*model.Service, error) {
	// Get actual service by name
	name, err := parseHostname(hostname)
	if err != nil {
		log.Infof("parseHostname(%s) => error %v", hostname, err)
		return nil, err
	}

	service, err := c.client.getService(name)
	if err != nil {
		return nil, err
	}

	return convertService(service), nil
}

// InstancesByPort retrieves instances for a service that match
// any of the supplied labels. All instances match an empty tag list.
// 根据主机名，服务端口和标签查询服务实例
func (c *Controller) InstancesByPort(hostname model.Hostname, port int,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	// Get actual service by name
	name, err := parseHostname(hostname)
	if err != nil {
		log.Infof("parseHostname(%s) => error %v", hostname, err)
		return nil, err
	}
	service, err := c.client.getService(name)
	if err != nil {
		return nil, err
	}
	var instances []*model.ServiceInstance
	hosts := service.Hosts
	for _, host := range hosts {
		instance := convertInstance(host)
		if labels.HasSubsetOf(instance.Labels) && (port == 0 || port == int(host.Port)) {
			instances = append(instances, instance)
		}
	}
	return instances, nil
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
// 查询边车代理所在节点上的服务实例
func (c *Controller) GetProxyServiceInstances(node *model.Proxy) ([]*model.ServiceInstance, error) {
	data, err := c.client.getAllServices()
	if err != nil {
		return nil, err
	}
	out := make([]*model.ServiceInstance, 0)

	for _, service := range data {
		hosts := service.Hosts
		for _, host := range hosts {
			addr := host.Ip
			if len(node.IPAddresses) > 0 {
				for _, ipAddress := range node.IPAddresses {
					if ipAddress == addr {
						out = append(out, convertInstance(host))
						break
					}
				}
			}
		}
	}
	return out, nil
}

//启动Controller的主循环，对Service 的变化进行分发
func (c *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) (model.LabelsCollection, error) {
	data, err := c.client.getAllServices()
	if err != nil {
		return nil, err
	}
	out := make(model.LabelsCollection, 0)
	for _, service := range data {
		hosts := service.Hosts
		for _, host := range hosts {
			addr := host.Ip
			if len(proxy.IPAddresses) > 0 {
				for _, ipAddress := range proxy.IPAddresses {
					if ipAddress == addr {
						labels := convertLabels(host.Metadata[SERVICE_TAGS])
						out = append(out, labels)
						break
					}
				}
			}
		}
	}

	return out, nil
}

// ManagementPorts retrieves set of health check ports by instance IP.
// This does not apply to Consul service registry, as Consul does not
// manage the service instances. In future, when we integrate Nomad, we
// might revisit this function.
func (c *Controller) ManagementPorts(addr string) model.PortList {
	return nil
}

// WorkloadHealthCheckInfo retrieves set of health check info by instance IP.
// This does not apply to Consul service registry, as Consul does not
// manage the service instances. In future, when we integrate Nomad, we
// might revisit this function.
func (c *Controller) WorkloadHealthCheckInfo(addr string) model.ProbeList {
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
		spiffe.MustGenSpiffeURI("default", "default"),
	}
}

//=================== Controller Interface =============//
// AppendServiceHandler(f func(*Service, Event)) error
// AppendInstanceHandler(f func(*ServiceInstance, Event)) error
// Run(stop <-chan struct{})

// Run all controllers until a signal is received
func (c *Controller) Run(stop <-chan struct{}) {
	cacheServices := make([]nacos_model.Service, 0)
	ticker := time.NewTicker(c.duration)
	for {
		select {
		case <-ticker.C:
			services, err := c.client.getAllServices()
			if err != nil {
				log.Warnf("periodic Eureka poll failed: %v", err)
				continue
			}

			addedService, deletedService, existServiceMap := getChangedServices(cacheServices, services)

			// 控制新增Service
			if len(addedService) > 0 {
				for _, h := range c.serviceHandlers {
					for _, data := range addedService {
						go h(convertService(data), model.EventAdd)
					}
				}
			}

			// 控制被删除Service
			if len(deletedService) > 0 {
				for _, h := range c.serviceHandlers {
					for _, data := range deletedService {
						go h(convertService(data), model.EventDelete)
					}
				}
			}

			// 目前根据instance是否变化来判断Service是否Update

			if existServiceMap != nil && len(existServiceMap) > 0 {
				for newData, oldData := range existServiceMap {
					addedInstance, deletedInstance, existInstanceMap := getChangedInstances(oldData.Hosts, newData.Hosts)

					var changed bool
					if addedInstance != nil && len(addedInstance) > 0 {
						changed = true
						for _, h := range c.instanceHandlers {
							for _, data := range addedInstance {
								go h(convertInstance(data), model.EventAdd)
							}
						}
					}
					if deletedService != nil && len(deletedService) > 0 {
						changed = true
						for _, h := range c.instanceHandlers {
							for _, data := range deletedInstance {
								go h(convertInstance(data), model.EventDelete)
							}
						}
					}

					if existInstanceMap != nil && len(existInstanceMap) > 0 {
						//判断实例是否发生变化
						for newInstance, oldInstance := range existInstanceMap {
							if !reflect.DeepEqual(newInstance.Metadata, oldInstance.Metadata) {
								changed = true
								for _, h := range c.instanceHandlers {
									go h(convertInstance(newInstance), model.EventUpdate)
								}
							}
						}
					}

					if changed {
						for _, h := range c.serviceHandlers {
							go h(convertService(newData), model.EventUpdate)
						}
					}
				}
			}
		case <-stop:
			ticker.Stop()
			return
		}
	}
}
func getChangedServices(oldCache []nacos_model.Service, newCache []nacos_model.Service) ([]nacos_model.Service, []nacos_model.Service, map[nacos_model.Service]nacos_model.Service) {
	if len(oldCache) == 0 {
		return newCache, []nacos_model.Service{}, map[nacos_model.Service]nacos_model.Service{}
	}
	if len(newCache) == 0 {
		return []nacos_model.Service{}, oldCache, map[nacos_model.Service]nacos_model.Service{}
	}

	mapService := make(map[string]nacos_model.Service, 0)
	for _, data := range oldCache {
		mapService[data.Name] = data
	}

	addedResult := make([]nacos_model.Service, 0)
	existResult := make([]nacos_model.Service, 0)
	existResultMap := make(map[string]nacos_model.Service, 0)
	for _, data := range newCache {
		data, ok := mapService[data.Name]
		if ok {
			existResult = append(existResult, data)
			existResultMap[data.Name] = data
		} else {
			addedResult = append(addedResult, data)
		}
	}

	deletedResult := make(map[nacos_model.Service]nacos_model.Service, 0)
	for _, oldData := range oldCache {
		newData, ok := existResultMap[oldData.Name]
		if ok {
			continue
		}
		deletedResult[newData] = oldData
	}

	return addedResult, existResult, deletedResult
}

func getChangedInstances(oldCache []nacos_model.Instance, newCache []nacos_model.Instance) ([]nacos_model.Instance, []nacos_model.Instance, map[nacos_model.Instance]nacos_model.Instance) {
	if len(oldCache) == 0 {
		return newCache, []nacos_model.Instance{}, map[nacos_model.Instance]nacos_model.Instance{}
	}
	if len(newCache) == 0 {
		return []nacos_model.Instance{}, oldCache, map[nacos_model.Instance]nacos_model.Instance{}
	}

	mapService := make(map[string]nacos_model.Instance, 0)
	for _, data := range oldCache {
		mapService[data.InstanceId] = data
	}

	addedResult := make([]nacos_model.Instance, 0)
	existResult := make([]nacos_model.Instance, 0)
	existResultMap := make(map[string]nacos_model.Instance, 0)
	for _, data := range newCache {
		data, ok := mapService[data.InstanceId]
		if ok {
			existResult = append(existResult, data)
			existResultMap[data.InstanceId] = data
		} else {
			addedResult = append(addedResult, data)
		}
	}

	deletedResult := make(map[nacos_model.Instance]nacos_model.Instance, 0)
	for _, oldData := range oldCache {
		newData, ok := existResultMap[oldData.InstanceId]
		if ok {
			continue
		}
		deletedResult[newData] = oldData
	}

	return addedResult, existResult, deletedResult
}
