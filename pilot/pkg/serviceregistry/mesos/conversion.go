package mesos

import (
	"fmt"
	"github.com/harryge00/go-marathon"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/pkg/log"
	"strings"
)

// TODO: use TCP/UDP or other protocol
func convertProtocol(name string, protocols []string) model.Protocol {
	if len(protocols) == 0 {
		log.Warnf("No protocol value for port: %v", name)
		return model.ProtocolTCP
	}
	protocol := model.ParseProtocol(protocols[0])
	if protocol == model.ProtocolUnsupported {
		log.Warnf("unsupported protocol value: %s", protocols[0])
		return model.ProtocolTCP
	}
	return protocol
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

func serviceHostname(name string) model.Hostname {
	return model.Hostname(fmt.Sprintf("%s.%s", name, VIPSuffix))
}

// split label by comma, and check if service is in the split array
func hasService(label, service string) bool {
	if label == "" {
		return false
	}
	if label == service {
		return true
	}
	services := strings.Split(label, ",")
	for i := range services {
		if services[i] == service {
			return true
		}
	}
	return false
}

func parseNodeID(id string) (podName string, err error) {
	parts := strings.Split(id, ".")
	if len(parts) < 1 || parts[0] == "" {
		err = fmt.Errorf("missing pod name from node.ID %q", id)
		return
	}
	podName = parts[0]
	return
}

func getPortList(pod *marathon.Pod) model.PortList {
	if pod == nil {
		return nil
	}
	var portList model.PortList
	for _, con := range pod.Containers {
		for _, ep := range con.Endpoints {
			port := model.Port{
				Name:     ep.Name,
				Protocol: convertProtocol(ep.Name, ep.Protocol),
				Port:     ep.ContainerPort,
			}
			portList = append(portList, &port)
		}
	}
	return portList
}

func convertPodStatus(status *marathon.PodStatus, reqSvcPort int) []*model.ServiceInstance {
	if status == nil {
		return nil
	}
	for _, container := range status.Spec.Containers {
		for _, ep := range container.Endpoints {
			if ep.ContainerPort == reqSvcPort {
				return getServiceInstance(status, ep)
			}
		}
	}
	return nil
}

func getServiceInstance(status *marathon.PodStatus, ep *marathon.PodEndpoint) []*model.ServiceInstance {
	portList := getPortList(status.Spec)
	service := model.Service{
		Hostname: serviceHostname(status.Spec.Labels[PodServiceLabel]),
		// TODO: use VIP IP as address
		Address:      model.UnspecifiedIP,
		Ports:        portList,
		MeshExternal: false,
		Resolution:   model.ClientSideLB,
		Attributes: model.ServiceAttributes{
			Name:      status.Spec.Labels[PodServiceLabel],
			Namespace: model.IstioDefaultConfigNamespace,
		},
	}
	var out []*model.ServiceInstance
	for _, inst := range status.Instances {
		for _, network := range inst.Networks {
			for _, addr := range network.Addresses {
				svcInst := model.ServiceInstance{
					Endpoint: model.NetworkEndpoint{
						Address: addr,
						Port:    ep.ContainerPort,
						ServicePort: &model.Port{
							Name:     ep.Name,
							Protocol: convertProtocol(ep.Name, ep.Protocol),
							Port:     ep.ContainerPort,
						},
					},
					//AvailabilityZone: "default",
					Service: &service,
					Labels:  status.Spec.Labels,
				}
				out = append(out, &svcInst)
			}
		}
	}
	return out
}
