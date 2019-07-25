package nacos

import (
	nacos_model "github.com/nacos-group/nacos-sdk-go/model"
	istio_model "istio.io/istio/pilot/pkg/model"
	"testing"
)

var (
	test_reviews = []nacos_model.Instance{
		{
			Valid:       true,
			Ip:          "172.19.0.6",
			Port:        9081,
			ServiceName: "nacos_istio_groupname@@reviews@@nacos_istio_cluster",
			Ephemeral:   true,
			Metadata:    map[string]string{SERVICE_TAGS: "version|v1", PROTOCOL_NAME: "tcp"},
		},
		{
			Valid:       true,
			Ip:          "172.19.0.7",
			Port:        9081,
			ServiceName: "nacos_istio_groupname@@reviews@@nacos_istio_cluster",
			Ephemeral:   true,
			Metadata:    map[string]string{SERVICE_TAGS: "version|v2", PROTOCOL_NAME: "tcp"},
		},
		{
			Valid:       true,
			Ip:          "172.19.0.8",
			Port:        9081,
			ServiceName: "nacos_istio_groupname@@reviews@@nacos_istio_cluster",
			Ephemeral:   true,
			Metadata:    map[string]string{SERVICE_TAGS: "version|v3", PROTOCOL_NAME: "tcp"},
		},
	}

	test_Service = nacos_model.Service{
		Name:  "test-Name",
		Hosts: test_reviews,
	}
)

func TestConvertService(t *testing.T) {
	istio_service := convertService(test_Service)
	if istio_service.Hostname != serviceHostname(test_Service.Name) {
		t.Errorf("convertService() bad hostname => %q, want %q",
			istio_service.Hostname, serviceHostname(test_Service.Name))
	}

	if istio_service.External() {
		t.Error("convertService() should not be an external service")
	}

	if len(istio_service.Ports) != 1 {
		t.Errorf("convertService() incorrect # of ports => %v, want %v",
			len(istio_service.Ports), 1)
	}
}

func TestConvertPort(t *testing.T) {
	ip := "172.19.0.6"
	serviceName := "reviews"
	protocl := "udp"
	instance := nacos_model.Instance{
		Valid:       true,
		Ip:          ip,
		Port:        9080,
		ServiceName: serviceName,
		Ephemeral:   true,
		Metadata:    map[string]string{SERVICE_TAGS: "version|v1"},
	}
	port := convertPort(instance, protocl)
	if port.Name != protocl {
		t.Errorf("convertPort() bad name => %v, want %v", port.Name, protocl)
	}
	if port.Protocol != istio_model.ProtocolUDP {
		t.Errorf("convertPort() bad protocol => %v, want %v", port.Protocol, istio_model.ProtocolUDP)
	}
}

func TestConvertInstance(t *testing.T) {
	instance := nacos_model.Instance{
		ClusterName: CLUSTER_NAME,
		Valid:       true,
		Ip:          "172.19.0.6",
		Port:        9080,
		ServiceName: "reviews",
		Ephemeral:   true,
		Metadata:    map[string]string{SERVICE_TAGS: "version|v1,zone|prod", PROTOCOL_NAME: "udp"},
	}
	serviceInstance := convertInstance(instance)

	if serviceInstance.Endpoint.ServicePort.Protocol != istio_model.ProtocolUDP {
		t.Errorf("convertInstance() => %v, want %v", serviceInstance.Endpoint.ServicePort.Protocol, istio_model.ProtocolUDP)
	}

	if serviceInstance.Endpoint.ServicePort.Name != "udp" {
		t.Errorf("convertInstance() => %v, want %v", serviceInstance.Endpoint.ServicePort.Name, "udp")
	}

	if serviceInstance.Endpoint.ServicePort.Port != 9080 {
		t.Errorf("convertInstance() => %v, want %v", serviceInstance.Endpoint.ServicePort.Port, 9080)
	}

	if serviceInstance.Endpoint.Locality != CLUSTER_NAME {
		t.Errorf("convertInstance() => %v, want %v", serviceInstance.Endpoint.Locality, CLUSTER_NAME)
	}

	if serviceInstance.Endpoint.Address != "172.19.0.6" {
		t.Errorf("convertInstance() => %v, want %v", serviceInstance.Endpoint.Address, "172.19.0.6")
	}

	if len(serviceInstance.Labels) != 2 {
		t.Errorf("convertInstance() len(Labels) => %v, want %v", len(serviceInstance.Labels), 2)
	}

	if serviceInstance.Labels["version"] != "v1" || serviceInstance.Labels["zone"] != "prod" {
		t.Errorf("convertInstance() => missing or incorrect tag in %q", serviceInstance.Labels)
	}

	if serviceInstance.Service.Hostname != serviceHostname("reviews") {
		t.Errorf("convertInstance() bad service hostname => %q, want %q",
			serviceInstance.Service.Hostname, serviceHostname("reviews"))
	}

	if serviceInstance.Service.Address != "172.19.0.6" {
		t.Errorf("convertInstance() bad service address => %q, want %q", serviceInstance.Service.Address, "172.19.0.6")
	}

	if len(serviceInstance.Service.Ports) != 1 {
		t.Errorf("convertInstance() incorrect # of service ports => %q, want %q", len(serviceInstance.Service.Ports), 1)
	}

	if serviceInstance.Service.Ports[0].Port != 9080 || serviceInstance.Service.Ports[0].Name != "udp" {
		t.Errorf("convertInstance() incorrect service port => %q", serviceInstance.Service.Ports[0])
	}

	if serviceInstance.Service.External() {
		t.Error("convertInstance() should not be external service")
	}
}
