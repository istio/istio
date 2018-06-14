package zookeeper

import "strconv"

type ServiceEventType int

type ServiceEvent struct {
	EventType ServiceEventType
	Service   *Service
	Instance  *Instance
}

type Port struct {
	Protocol string
	Port     string
}

type Service struct {
	name      string
	ports     []*Port
	instances map[string]*Instance
}

// Instance is the instance of the service provider
// instance lables includes:
// 		appName         name of the application which host the service itself
// 		language		language the service is build with
// 		rpcVer			version of the sofa rpc framework
// 		dynamic			...
// 		tartTime		time when this instance is started
// 		version			version of this service instance
// 		accepts			...
// 		delay			...
// 		weight			route weight of this instance
// 		timeout			server side timeout
// 		id				id of the service, already deprecated
// 		pid				process id of the service instance
// 		uniqueId		unique id of the service
type Instance struct {
	Service *Service
	Host    string
	Port    *Port
	Labels  map[string]string
}

const (
	ServiceAdded           ServiceEventType = iota
	ServiceDeleted
	ServiceInstanceAdded
	ServiceInstanceDeleted
)

func (p *Port) Portoi() int {
	port, err := strconv.Atoi(p.Port)
	if err != nil {
		return 0
	}
	return port
}

func (s *Service) AddPort(port *Port) {
	exist := false
	for _, p := range s.ports {
		if p.Port == port.Port && p.Protocol == port.Protocol {
			exist = true
			break
		}
	}
	if !exist {
		s.ports = append(s.ports, port)
	}
}

func (s *Service) Hostname() string {
	return s.name
}

func (s *Service) Ports() []*Port {
	return s.ports
}

func (s *Service) Instances() map[string]*Instance {
	return s.instances
}
