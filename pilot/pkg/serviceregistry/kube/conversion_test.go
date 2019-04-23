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
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/spiffe"
)

var (
	domainSuffix = "company.com"
)

func TestConvertProtocol(t *testing.T) {
	type protocolCase struct {
		name  string
		proto v1.Protocol
		out   model.Protocol
	}
	protocols := []protocolCase{
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
		{"grpc-web", v1.ProtocolTCP, model.ProtocolGRPCWeb},
		{"grpc-web-test", v1.ProtocolTCP, model.ProtocolGRPCWeb},
		{"mongo", v1.ProtocolTCP, model.ProtocolMongo},
		{"mongo-test", v1.ProtocolTCP, model.ProtocolMongo},
		{"redis", v1.ProtocolTCP, model.ProtocolRedis},
		{"redis-test", v1.ProtocolTCP, model.ProtocolRedis},
		{"mysql", v1.ProtocolTCP, model.ProtocolMySQL},
		{"mysql-test", v1.ProtocolTCP, model.ProtocolMySQL},
	}

	// Create the list of cases for all of the names in both upper and lowercase.
	cases := make([]protocolCase, 0, len(protocols)*2)
	for _, p := range protocols {
		name := p.name

		p.name = strings.ToLower(name)
		cases = append(cases, p)

		// Don't bother adding uppercase version for empty string.
		if name != "" {
			p.name = strings.ToUpper(name)
			cases = append(cases, p)
		}
	}

	for _, c := range cases {
		testName := strings.Replace(fmt.Sprintf("%s_%s", c.name, c.proto), "-", "_", -1)
		t.Run(testName, func(t *testing.T) {
			out := ConvertProtocol(c.name, c.proto)
			if out != c.out {
				t.Errorf("convertProtocol(%q, %q) => %q, want %q", c.name, c.proto, out, c.out)
			}
		})
	}
}

func BenchmarkConvertProtocol(b *testing.B) {
	cases := []struct {
		name  string
		proto v1.Protocol
		out   model.Protocol
	}{
		{"grpc-web-lowercase", v1.ProtocolTCP, model.ProtocolGRPCWeb},
		{"GRPC-WEB-mixedcase", v1.ProtocolTCP, model.ProtocolGRPCWeb},
		{"https-lowercase", v1.ProtocolTCP, model.ProtocolHTTPS},
		{"HTTPS-mixedcase", v1.ProtocolTCP, model.ProtocolHTTPS},
	}

	for _, c := range cases {
		testName := strings.Replace(c.name, "-", "_", -1)
		b.Run(testName, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				out := ConvertProtocol(c.name, c.proto)
				if out != c.out {
					b.Fatalf("convertProtocol(%q, %q) => %q, want %q", c.name, c.proto, out, c.out)
				}
			}
		})
	}
}

func TestServiceConversion(t *testing.T) {
	serviceName := "service1"
	namespace := "default"
	saA := "serviceaccountA"
	saB := "serviceaccountB"
	saC := "spiffe://accounts.google.com/serviceaccountC@cloudservices.gserviceaccount.com"
	saD := "spiffe://accounts.google.com/serviceaccountD@developer.gserviceaccount.com"

	oldTrustDomain := spiffe.GetTrustDomain()
	spiffe.SetTrustDomain(domainSuffix)
	defer spiffe.SetTrustDomain(oldTrustDomain)

	ip := "10.0.0.1"

	tnow := time.Now()
	localSvc := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
			Annotations: map[string]string{
				KubeServiceAccountsOnVMAnnotation:  saA + "," + saB,
				CanonicalServiceAccountsAnnotation: saC + "," + saD,
				"other/annotation":                 "test",
			},
			CreationTimestamp: metav1.Time{tnow},
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

	if service.CreationTime != tnow {
		t.Errorf("incorrect creation time => %v, want %v", service.CreationTime, tnow)
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
		saC, saD,
		"spiffe://company.com/ns/default/sa/" + saA,
		"spiffe://company.com/ns/default/sa/" + saB,
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

	if !service.External() {
		t.Error("service should be external")
	}

	if service.Hostname != serviceHostname(serviceName, namespace, domainSuffix) {
		t.Errorf("service hostname incorrect => %q, want %q",
			service.Hostname, serviceHostname(serviceName, namespace, domainSuffix))
	}
}

func TestExternalClusterLocalServiceConversion(t *testing.T) {
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
			ExternalName: "some.test.svc.cluster.local",
		},
	}

	domainSuffix := "cluster.local"

	service := convertService(extSvc, domainSuffix)
	if service == nil {
		t.Errorf("could not convert external service")
	}

	if len(service.Ports) != len(extSvc.Spec.Ports) {
		t.Errorf("incorrect number of ports => %v, want %v",
			len(service.Ports), len(extSvc.Spec.Ports))
	}

	if !service.External() {
		t.Error("ExternalName service (even if .cluster.local) should be external")
	}

	if service.Hostname != serviceHostname(serviceName, namespace, domainSuffix) {
		t.Errorf("service hostname incorrect => %q, want %q",
			service.Hostname, serviceHostname(serviceName, namespace, domainSuffix))
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

func TestSecureNamingSANCustomIdentity(t *testing.T) {

	pod := &v1.Pod{}

	identity := "foo"

	pod.Annotations = make(map[string]string)
	pod.Annotations[IdentityPodAnnotation] = identity

	san := secureNamingSAN(pod)

	expectedSAN := fmt.Sprintf("spiffe://%v/%v", spiffe.GetTrustDomain(), identity)

	if san != expectedSAN {
		t.Errorf("SAN match failed, SAN:%v  expectedSAN:%v", san, expectedSAN)
	}

}

func TestSecureNamingSAN(t *testing.T) {

	pod := &v1.Pod{}

	pod.Annotations = make(map[string]string)

	ns := "anything"
	sa := "foo"
	pod.Namespace = ns
	pod.Spec.ServiceAccountName = sa

	san := secureNamingSAN(pod)

	expectedSAN := fmt.Sprintf("spiffe://%v/ns/%v/sa/%v", spiffe.GetTrustDomain(), ns, sa)

	if san != expectedSAN {
		t.Errorf("SAN match failed, SAN:%v  expectedSAN:%v", san, expectedSAN)
	}

}
