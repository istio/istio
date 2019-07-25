package nacos

import (
	"fmt"
	nacos_model "github.com/nacos-group/nacos-sdk-go/model"
	istio_model "istio.io/istio/pilot/pkg/model"
	"istio.io/pkg/log"
	"sort"
	"strings"
)

func convertService(service nacos_model.Service) *istio_model.Service {

	name := service.Name
	meshExternal := false
	resolution := istio_model.ClientSideLB

	ports := make(map[int]*istio_model.Port)

	hosts := service.Hosts
	for _, instance := range hosts {
		port := convertPort(instance, instance.Metadata[PROTOCOL_NAME])
		if svcPort, exists := ports[port.Port]; exists && svcPort.Protocol != port.Protocol {
			log.Warnf("Service %v has two instances on same port %v but different protocols (%v, %v)",
				name, port.Port, svcPort.Protocol, port.Protocol)
		} else {
			ports[port.Port] = port
		}

		if instance.Metadata[EXTERNAL_NAME] != "" {
			meshExternal = true
			resolution = istio_model.Passthrough
		}
	}

	svcPorts := make(istio_model.PortList, 0, len(ports))
	for _, port := range ports {
		svcPorts = append(svcPorts, port)
	}

	hostName := serviceHostname(formatServiceName(name))
	result := &istio_model.Service{
		Hostname:     hostName,
		Address:      istio_model.UnspecifiedIP,
		Ports:        svcPorts,
		MeshExternal: meshExternal,
		Resolution:   resolution,
		Attributes: istio_model.ServiceAttributes{
			Name:      string(hostName),
			Namespace: istio_model.IstioDefaultConfigNamespace,
		},
	}
	return result
}

func convertPort(instance nacos_model.Instance, name string) *istio_model.Port {
	if name == "" {
		name = "tcp"
	}

	var result istio_model.Port

	result = istio_model.Port{
		Name:     name,
		Port:     int(instance.Port),
		Protocol: convertProtocol(name),
	}
	return &result
}

func convertInstance(instance nacos_model.Instance) *istio_model.ServiceInstance {
	labels := convertLabels(instance.Metadata[SERVICE_TAGS])
	port := convertPort(instance, instance.Metadata[PROTOCOL_NAME])

	addr := instance.Ip

	meshExternal := false
	resolution := istio_model.ClientSideLB
	externalName := instance.Metadata[EXTERNAL_NAME]
	if externalName != "" {
		meshExternal = true
		resolution = istio_model.DNSLB
	}

	hostname := serviceHostname(formatServiceName(instance.ServiceName))
	return &istio_model.ServiceInstance{
		Endpoint: istio_model.NetworkEndpoint{
			Address:     addr,
			Port:        int(instance.Port),
			ServicePort: port,
			Locality:    instance.ClusterName,
		},
		Service: &istio_model.Service{
			Hostname:     hostname,
			Address:      instance.Ip,
			Ports:        istio_model.PortList{port},
			MeshExternal: meshExternal,
			Resolution:   resolution,
			Attributes: istio_model.ServiceAttributes{
				Name:      string(hostname),
				Namespace: istio_model.IstioDefaultConfigNamespace,
			},
		},
		Labels: labels,
	}
}

func convertLabels(tags string) istio_model.Labels {
	labels := strings.Split(tags, ",")
	out := make(istio_model.Labels, len(labels))
	for _, tag := range labels {
		vals := strings.Split(tag, "|")
		// Labels not of form "key|value" are ignored to avoid possible collisions
		if len(vals) > 1 {
			out[vals[0]] = vals[1]
		} else {
			log.Warnf("Tag %v ignored since it is not of form key|value", tag)
		}
	}
	return out
}

func convertProtocol(name string) istio_model.Protocol {
	protocol := istio_model.ParseProtocol(name)
	if protocol == istio_model.ProtocolUnsupported {
		log.Warnf("unsupported protocol value: %s", name)
		return istio_model.ProtocolTCP
	}
	return protocol
}

func sortServices(services []nacos_model.Service) {
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	for _, service := range services {
		sortInstances(service.Hosts)
	}
}

func sortInstances(instances []nacos_model.Instance) {
	sort.Slice(instances, func(i, j int) bool {

		if instances[i].Ip == instances[j].Ip {
			if instances[i].Port == instances[j].Port {
				return instances[i].InstanceId < instances[j].InstanceId
			}
			return instances[i].Port < instances[j].Port
		}
		return instances[i].Ip < instances[j].Ip
	})
}

func serviceHostname(name string) istio_model.Hostname {
	return istio_model.Hostname(fmt.Sprintf("%s.service.nacos", name))
}

func parseHostname(hostname istio_model.Hostname) (name string, err error) {
	parts := strings.Split(string(hostname), ".")
	if len(parts) < 1 || parts[0] == "" {
		err = fmt.Errorf("missing service name from the service hostname %q", hostname)
		return
	}
	name = parts[0]
	return
}

func formatServiceName(serviceName string) string {
	parts := strings.Split(string(serviceName), "@@")
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[1]
}
