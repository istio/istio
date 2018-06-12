package zookeeper

type ServiceEventType int

type ServiceEvent struct {
	EventType ServiceEventType
	Service   string
	Instance  *Instance
}

type Service struct {
	name      string
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
	Service  string
	Protocol string
	Host     string
	Port     string
	Labels   map[string]string
}

const (
	ServiceAdded           ServiceEventType = iota
	ServiceDeleted
	ServiceInstanceAdded
	ServiceInstanceDeleted
)
