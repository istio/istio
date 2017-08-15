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
	"sort"
	"testing"

	"istio.io/pilot/model"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	}
)

func TestConvertProtocol(t *testing.T) {
	for _, tt := range protocols {
		out := convertProtocol(tt.name, tt.proto)
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
	sort.Sort(sort.StringSlice(sa))
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

func TestInvalidServiceConversion(t *testing.T) {
	serviceName := "service1"
	namespace := "default"

	localSvc := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
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

	if svc := convertService(localSvc, domainSuffix); svc != nil {
		t.Errorf("converted a service without a cluster IP")
	}
}

func TestInvalidExternalServiceConversion(t *testing.T) {
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
			Type: v1.ServiceTypeExternalName,
		},
	}

	if svc := convertService(extSvc, domainSuffix); svc != nil {
		t.Errorf("converted a service without an external name")
	}
}
