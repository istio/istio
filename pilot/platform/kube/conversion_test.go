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

package kube

import (
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/pilot/model"
)

var (
	domainSuffix = "company.com"

	protocols = []struct {
		name  string
		proto v1.Protocol
		out   model.Protocol
	}{
		{"", v1.ProtocolTCP, model.ProtocolTCP},
		{"http", v1.ProtocolTCP, model.ProtocolHTTP},
		{"http-test", v1.ProtocolTCP, model.ProtocolHTTP},
		{"http", v1.ProtocolUDP, model.ProtocolUDP},
		{"httptest", v1.ProtocolTCP, model.ProtocolTCP},
		{"https", v1.ProtocolTCP, model.ProtocolHTTPS},
		{"https-test", v1.ProtocolTCP, model.ProtocolHTTPS},
		{"http2", v1.ProtocolTCP, model.ProtocolHTTP2},
		{"http2-test", v1.ProtocolTCP, model.ProtocolHTTP2},
		{"grpc", v1.ProtocolTCP, model.ProtocolGRPC},
		{"grpc-test", v1.ProtocolTCP, model.ProtocolGRPC},
		{"mongo", v1.ProtocolTCP, model.ProtocolMONGO},
		{"mongo-test", v1.ProtocolTCP, model.ProtocolMONGO},
	}
)

func TestConvertProtocol(t *testing.T) {
	for _, tt := range protocols {
		out := ConvertProtocol(tt.name, tt.proto)
		if out != tt.out {
			t.Errorf("convertProtocol(%q, %q) => %q, want %q", tt.name, tt.proto, out, tt.out)
		}
	}
}

func TestServiceConversion(t *testing.T) {
	serviceName := "service1"
	namespace := "default"
	saA := "serviceaccountA"
	saB := "serviceaccountB"
	saC := "serviceaccountC@cloudservices.gserviceaccount.com"
	saD := "serviceaccountD@developer.gserviceaccount.com"

	ip := "10.0.0.1"

	localSvc := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
			Annotations: map[string]string{
				KubeServiceAccountsOnVMAnnotation:      saA + "," + saB,
				CanonicalServiceAccountsOnVMAnnotation: saC + "," + saD,
				"other/annotation":                     "test",
			},
		},
		Spec: v1.ServiceSpec{
			ClusterIP: ip,
			Ports: []v1.ServicePort{
				{
					Name:     "http",
					Port:     8080,
					Protocol: v1.ProtocolTCP,
				},
				{
					Name:     "https",
					Protocol: v1.ProtocolTCP,
					Port:     443,
				},
			},
		},
	}

	service := convertService(localSvc, domainSuffix)
	if service == nil {
		t.Errorf("could not convert service")
	}

	if len(service.Ports) != len(localSvc.Spec.Ports) {
		t.Errorf("incorrect number of ports => %v, want %v",
			len(service.Ports), len(localSvc.Spec.Ports))
	}

	if service.External() {
		t.Error("service should not be external")
	}

	if service.Hostname != serviceHostname(serviceName, namespace, domainSuffix) {
		t.Errorf("service hostname incorrect => %q, want %q",
			service.Hostname, serviceHostname(serviceName, namespace, domainSuffix))
	}

	if service.Address != ip {
		t.Errorf("service IP incorrect => %q, want %q", service.Address, ip)
	}

	sa := service.ServiceAccounts
	if sa == nil || len(sa) != 4 {
		t.Errorf("number of service accounts is incorrect")
	}
	expected := []string{
		"spiffe://company.com/ns/default/sa/" + saA,
		"spiffe://company.com/ns/default/sa/" + saB,
		"spiffe://" + saC,
		"spiffe://" + saD,
	}
	if !reflect.DeepEqual(sa, expected) {
		t.Errorf("Unexpected service accounts %v (expecting %v)", sa, expected)
	}
}

func TestServiceConversionWithEmptyServiceAccountsAnnotation(t *testing.T) {
	serviceName := "service1"
	namespace := "default"

	ip := "10.0.0.1"

	localSvc := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Namespace:   namespace,
			Annotations: map[string]string{},
		},
		Spec: v1.ServiceSpec{
			ClusterIP: ip,
			Ports: []v1.ServicePort{
				{
					Name:     "http",
					Port:     8080,
					Protocol: v1.ProtocolTCP,
				},
				{
					Name:     "https",
					Protocol: v1.ProtocolTCP,
					Port:     443,
				},
			},
		},
	}

	service := convertService(localSvc, domainSuffix)
	if service == nil {
		t.Errorf("could not convert service")
	}

	sa := service.ServiceAccounts
	if len(sa) != 0 {
		t.Errorf("number of service accounts is incorrect: %d, expected 0", len(sa))
	}
}

func TestExternalServiceConversion(t *testing.T) {
	serviceName := "service1"
	namespace := "default"

	extSvc := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					Protocol: v1.ProtocolTCP,
				},
			},
			Type:         v1.ServiceTypeExternalName,
			ExternalName: "google.com",
		},
	}

	service := convertService(extSvc, domainSuffix)
	if service == nil {
		t.Errorf("could not convert external service")
	}

	if len(service.Ports) != len(extSvc.Spec.Ports) {
		t.Errorf("incorrect number of ports => %v, want %v",
			len(service.Ports), len(extSvc.Spec.Ports))
	}

	if service.ExternalName != extSvc.Spec.ExternalName || !service.External() {
		t.Error("service should be external")
	}

	if service.Hostname != serviceHostname(serviceName, namespace, domainSuffix) {
		t.Errorf("service hostname incorrect => %q, want %q",
			service.Hostname, extSvc.Spec.ExternalName)
	}
}

func TestProbesToPortsConversion(t *testing.T) {

	expected := model.PortList{
		{
			Name:     "mgmt-3306",
			Port:     3306,
			Protocol: model.ProtocolTCP,
		},
		{
			Name:     "mgmt-9080",
			Port:     9080,
			Protocol: model.ProtocolHTTP,
		},
	}

	handlers := []v1.Handler{
		{
			TCPSocket: &v1.TCPSocketAction{
				Port: intstr.IntOrString{StrVal: "mysql", Type: intstr.String},
			},
		},
		{
			TCPSocket: &v1.TCPSocketAction{
				Port: intstr.IntOrString{IntVal: 3306, Type: intstr.Int},
			},
		},
		{
			HTTPGet: &v1.HTTPGetAction{
				Path: "/foo",
				Port: intstr.IntOrString{StrVal: "http-two", Type: intstr.String},
			},
		},
		{
			HTTPGet: &v1.HTTPGetAction{
				Path: "/foo",
				Port: intstr.IntOrString{IntVal: 9080, Type: intstr.Int},
			},
		},
	}

	podSpec := &v1.PodSpec{
		Containers: []v1.Container{
			{
				Name: "scooby",
				Ports: []v1.ContainerPort{
					{
						Name:          "mysql",
						ContainerPort: 3306,
					},
					{
						Name:          "http-two",
						ContainerPort: 9080,
					},
					{
						Name:          "http",
						ContainerPort: 80,
					},
				},
				LivenessProbe:  &v1.Probe{},
				ReadinessProbe: &v1.Probe{},
			},
		},
	}

	for _, handler1 := range handlers {
		for _, handler2 := range handlers {
			if (handler1.TCPSocket != nil && handler2.TCPSocket != nil) ||
				(handler1.HTTPGet != nil && handler2.HTTPGet != nil) {
				continue
			}

			podSpec.Containers[0].LivenessProbe.Handler = handler1
			podSpec.Containers[0].ReadinessProbe.Handler = handler2

			mgmtPorts, err := convertProbesToPorts(podSpec)
			if err != nil {
				t.Errorf("Failed to convert Probes to Ports: %v", err)
			}

			if !reflect.DeepEqual(mgmtPorts, expected) {
				t.Errorf("incorrect number of management ports => %v, want %v",
					len(mgmtPorts), len(expected))
			}
		}
	}
}
