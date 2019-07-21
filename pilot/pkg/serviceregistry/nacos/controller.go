package nacos

import (
	nacos_model "github.com/nacos-group/nacos-sdk-go/model"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/spiffe"
	"istio.io/pkg/log"
	"reflect"
	"strings"
	"time"
)

type serviceHandler func(*model.Service, model.Event)
type instanceHandler func(*model.ServiceInstance, model.Event)

type Controller struct {
	client           *Client
	serviceHandlers  []serviceHandler
	instanceHandlers []instanceHandler
	duration         time.Duration
}

func NewController(addr string, duration time.Duration) (*Controller, error) {
	client, err := NewClient(strings.ReplaceAll(addr,"http://","" ))
	return &Controller{
		client:           client,
		serviceHandlers:  make([]serviceHandler, 0),
		instanceHandlers: make([]instanceHandler, 0),
		duration:         duration,
	}, err
}

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

func (c *Controller) ManagementPorts(addr string) model.PortList {
	return nil
}

func (c *Controller) WorkloadHealthCheckInfo(addr string) model.ProbeList {
	return nil
}

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

func (c *Controller) Run(stop <-chan struct{}) {
	cacheServices := make([]nacos_model.Service, 0)
	ticker := time.NewTicker(c.duration)
	for {
		select {
		case <-ticker.C:
			services, err := c.client.getAllServices()
			if err != nil {
				log.Warnf("periodic Nacos poll failed: %v", err)
				continue
			}
			sortServices(services)
			if !reflect.DeepEqual(services, cacheServices) {
				cacheServices = services
				// TODO: feed with real events.
				// The handlers are being fed dummy events. This is sufficient with simplistic
				// handlers that invalidate the cache on any event but will not work with smarter
				// handlers.
				for _, h := range c.serviceHandlers {
					go h(&model.Service{}, model.EventAdd)
				}
				for _, h := range c.instanceHandlers {
					go h(&model.ServiceInstance{}, model.EventAdd)
				}
			}

		case <-stop:
			ticker.Stop()
			return
		}
	}
}

func (c *Controller) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.serviceHandlers = append(c.serviceHandlers, f)
	return nil
}

func (c *Controller) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.instanceHandlers = append(c.instanceHandlers, f)
	return nil
}