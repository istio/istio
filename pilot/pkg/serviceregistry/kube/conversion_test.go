// Copyright Istio Authors
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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/kube"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/util/sets"
)

var (
	domainSuffix = "company.com"
	clusterID    = cluster.ID("test-cluster")
)

func TestConvertProtocol(t *testing.T) {
	http := "http"
	type protocolCase struct {
		port        int32
		name        string
		appProtocol *string
		proto       corev1.Protocol
		out         protocol.Instance
	}
	protocols := []protocolCase{
		{8888, "", nil, corev1.ProtocolTCP, protocol.Unsupported},
		{25, "", nil, corev1.ProtocolTCP, protocol.TCP},
		{53, "", nil, corev1.ProtocolTCP, protocol.TCP},
		{3306, "", nil, corev1.ProtocolTCP, protocol.TCP},
		{27017, "", nil, corev1.ProtocolTCP, protocol.TCP},
		{8888, "http", nil, corev1.ProtocolTCP, protocol.HTTP},
		{8888, "http-test", nil, corev1.ProtocolTCP, protocol.HTTP},
		{8888, "http", nil, corev1.ProtocolUDP, protocol.UDP},
		{8888, "httptest", nil, corev1.ProtocolTCP, protocol.Unsupported},
		{25, "httptest", nil, corev1.ProtocolTCP, protocol.TCP},
		{53, "httptest", nil, corev1.ProtocolTCP, protocol.TCP},
		{3306, "httptest", nil, corev1.ProtocolTCP, protocol.TCP},
		{27017, "httptest", nil, corev1.ProtocolTCP, protocol.TCP},
		{8888, "https", nil, corev1.ProtocolTCP, protocol.HTTPS},
		{8888, "https-test", nil, corev1.ProtocolTCP, protocol.HTTPS},
		{8888, "http2", nil, corev1.ProtocolTCP, protocol.HTTP2},
		{8888, "http2-test", nil, corev1.ProtocolTCP, protocol.HTTP2},
		{8888, "grpc", nil, corev1.ProtocolTCP, protocol.GRPC},
		{8888, "grpc-test", nil, corev1.ProtocolTCP, protocol.GRPC},
		{8888, "grpc-web", nil, corev1.ProtocolTCP, protocol.GRPCWeb},
		{8888, "grpc-web-test", nil, corev1.ProtocolTCP, protocol.GRPCWeb},
		{8888, "mongo", nil, corev1.ProtocolTCP, protocol.Mongo},
		{8888, "mongo-test", nil, corev1.ProtocolTCP, protocol.Mongo},
		{8888, "redis", nil, corev1.ProtocolTCP, protocol.Redis},
		{8888, "redis-test", nil, corev1.ProtocolTCP, protocol.Redis},
		{8888, "mysql", nil, corev1.ProtocolTCP, protocol.MySQL},
		{8888, "mysql-test", nil, corev1.ProtocolTCP, protocol.MySQL},
		{8888, "tcp", &http, corev1.ProtocolTCP, protocol.HTTP},
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
		testName := strings.Replace(fmt.Sprintf("%s_%s_%d", c.name, c.proto, c.port), "-", "_", -1)
		t.Run(testName, func(t *testing.T) {
			out := kube.ConvertProtocol(c.port, c.name, c.proto, c.appProtocol)
			if out != c.out {
				t.Fatalf("convertProtocol(%d, %q, %q) => %q, want %q", c.port, c.name, c.proto, out, c.out)
			}
		})
	}
}

func BenchmarkConvertProtocol(b *testing.B) {
	cases := []struct {
		name  string
		proto corev1.Protocol
		out   protocol.Instance
	}{
		{"grpc-web-lowercase", corev1.ProtocolTCP, protocol.GRPCWeb},
		{"GRPC-WEB-mixedcase", corev1.ProtocolTCP, protocol.GRPCWeb},
		{"https-lowercase", corev1.ProtocolTCP, protocol.HTTPS},
		{"HTTPS-mixedcase", corev1.ProtocolTCP, protocol.HTTPS},
	}

	for _, c := range cases {
		testName := strings.Replace(c.name, "-", "_", -1)
		b.Run(testName, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				out := kube.ConvertProtocol(8888, c.name, c.proto, nil)
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

	ip := "10.0.0.1"

	tnow := time.Now()
	localSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
			Annotations: map[string]string{
				annotation.AlphaKubernetesServiceAccounts.Name: saA + "," + saB,
				annotation.AlphaCanonicalServiceAccounts.Name:  saC + "," + saD,
				"other/annotation": "test",
			},
			CreationTimestamp: metav1.Time{Time: tnow},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: ip,
			Selector:  map[string]string{"foo": "bar"},
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     8080,
					Protocol: corev1.ProtocolTCP,
				},
				{
					Name:     "https",
					Protocol: corev1.ProtocolTCP,
					Port:     443,
				},
			},
		},
	}

	service := ConvertService(localSvc, domainSuffix, clusterID, domainSuffix)
	if service == nil {
		t.Fatal("could not convert service")
	}

	if service.CreationTime != tnow {
		t.Fatalf("incorrect creation time => %v, want %v", service.CreationTime, tnow)
	}

	if len(service.Ports) != len(localSvc.Spec.Ports) {
		t.Fatalf("incorrect number of ports => %v, want %v",
			len(service.Ports), len(localSvc.Spec.Ports))
	}

	if service.External() {
		t.Fatal("service should not be external")
	}

	if service.Hostname != ServiceHostname(serviceName, namespace, domainSuffix) {
		t.Fatalf("service hostname incorrect => %q, want %q",
			service.Hostname, ServiceHostname(serviceName, namespace, domainSuffix))
	}

	ips := service.ClusterVIPs.GetAddressesFor(clusterID)
	if len(ips) != 1 {
		t.Fatalf("number of ips incorrect => %q, want 1", len(ips))
	}

	if ips[0] != ip {
		t.Fatalf("service IP incorrect => %q, want %q", ips[0], ip)
	}

	actualIPs := service.ClusterVIPs.GetAddressesFor(clusterID)
	expectedIPs := []string{ip}
	if !reflect.DeepEqual(actualIPs, expectedIPs) {
		t.Fatalf("service IPs incorrect => %q, want %q", actualIPs, expectedIPs)
	}

	if !reflect.DeepEqual(service.Attributes.LabelSelectors, localSvc.Spec.Selector) {
		t.Fatalf("service label selectors incorrect => %q, want %q", service.Attributes.LabelSelectors,
			localSvc.Spec.Selector)
	}

	sa := service.ServiceAccounts
	if sa == nil || len(sa) != 4 {
		t.Fatal("number of service accounts is incorrect")
	}
	expected := []string{
		saC, saD,
		"spiffe://company.com/ns/default/sa/" + saA,
		"spiffe://company.com/ns/default/sa/" + saB,
	}
	if !reflect.DeepEqual(sa, expected) {
		t.Fatalf("Unexpected service accounts %v (expecting %v)", sa, expected)
	}
}

func TestServiceConversionWithEmptyServiceAccountsAnnotation(t *testing.T) {
	serviceName := "service1"
	namespace := "default"

	ip := "10.0.0.1"

	localSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Namespace:   namespace,
			Annotations: map[string]string{},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: ip,
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     8080,
					Protocol: corev1.ProtocolTCP,
				},
				{
					Name:     "https",
					Protocol: corev1.ProtocolTCP,
					Port:     443,
				},
			},
		},
	}

	service := ConvertService(localSvc, domainSuffix, clusterID, "")
	if service == nil {
		t.Fatal("could not convert service")
	}

	sa := service.ServiceAccounts
	if len(sa) != 0 {
		t.Fatalf("number of service accounts is incorrect: %d, expected 0", len(sa))
	}
}

func TestServiceConversionWithExportToAnnotation(t *testing.T) {
	serviceName := "service1"
	namespace := "default"

	ip := "10.0.0.1"

	localSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Namespace:   namespace,
			Annotations: map[string]string{},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: ip,
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     8080,
					Protocol: corev1.ProtocolTCP,
				},
				{
					Name:     "https",
					Protocol: corev1.ProtocolTCP,
					Port:     443,
				},
			},
		},
	}

	tests := []struct {
		Annotation string
		Want       sets.Set[visibility.Instance]
	}{
		{"", sets.Set[visibility.Instance]{}},
		{".", sets.New(visibility.Private)},
		{"*", sets.New(visibility.Public)},
		{"~", sets.New(visibility.None)},
		{"ns", sets.New(visibility.Instance("ns"))},
		{"ns1,ns2", sets.New(visibility.Instance("ns1"), visibility.Instance("ns2"))},
		{"ns1, ns2", sets.New(visibility.Instance("ns1"), visibility.Instance("ns2"))},
		{"ns1 ,ns2", sets.New(visibility.Instance("ns1"), visibility.Instance("ns2"))},
	}
	for _, test := range tests {
		localSvc.Annotations[annotation.NetworkingExportTo.Name] = test.Annotation
		service := ConvertService(localSvc, domainSuffix, clusterID, "")
		if service == nil {
			t.Fatal("could not convert service")
		}

		if !service.Attributes.ExportTo.Equals(test.Want) {
			t.Errorf("incorrect exportTo conversion: got = %v, but want = %v", service.Attributes.ExportTo, test.Want)
		}
	}
}

func TestExternalServiceConversion(t *testing.T) {
	serviceName := "service1"
	namespace := "default"

	extSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					Protocol: corev1.ProtocolTCP,
				},
			},
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: "google.com",
		},
	}

	service := ConvertService(extSvc, domainSuffix, clusterID, "")
	if service == nil {
		t.Fatal("could not convert external service")
	}

	if len(service.Ports) != len(extSvc.Spec.Ports) {
		t.Fatalf("incorrect number of ports => %v, want %v",
			len(service.Ports), len(extSvc.Spec.Ports))
	}

	if !service.External() {
		t.Fatal("service should be external")
	}

	if service.Hostname != ServiceHostname(serviceName, namespace, domainSuffix) {
		t.Fatalf("service hostname incorrect => %q, want %q",
			service.Hostname, ServiceHostname(serviceName, namespace, domainSuffix))
	}

	if service.Attributes.Type != string(extSvc.Spec.Type) ||
		service.Attributes.ExternalName != extSvc.Spec.ExternalName {
		t.Fatalf("service attributes incorrect => %v/%v, want %v/%v",
			service.Attributes.Type, service.Attributes.ExternalName, extSvc.Spec.Type, extSvc.Spec.ExternalName)
	}
}

func TestExternalClusterLocalServiceConversion(t *testing.T) {
	serviceName := "service1"
	namespace := "default"

	extSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					Protocol: corev1.ProtocolTCP,
				},
			},
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: "some.test.svc.cluster.local",
		},
	}

	domainSuffix := "cluster.local"

	service := ConvertService(extSvc, domainSuffix, clusterID, "")
	if service == nil {
		t.Fatal("could not convert external service")
	}

	if len(service.Ports) != len(extSvc.Spec.Ports) {
		t.Fatalf("incorrect number of ports => %v, want %v",
			len(service.Ports), len(extSvc.Spec.Ports))
	}

	if !service.External() {
		t.Fatal("ExternalName service (even if .cluster.local) should be external")
	}

	if service.Hostname != ServiceHostname(serviceName, namespace, domainSuffix) {
		t.Fatalf("service hostname incorrect => %q, want %q",
			service.Hostname, ServiceHostname(serviceName, namespace, domainSuffix))
	}
}

func TestLBServiceConversion(t *testing.T) {
	serviceName := "service1"
	namespace := "default"

	addresses := []corev1.LoadBalancerIngress{
		{
			IP: "127.68.32.112",
		},
		{
			IP: "127.68.32.113",
		},
		{
			Hostname: "127.68.32.114",
		},
		{
			Hostname: "127.68.32.115",
		},
	}

	extSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					Protocol: corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeLoadBalancer,
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: addresses,
			},
		},
	}

	service := ConvertService(extSvc, domainSuffix, clusterID, "")
	if service == nil {
		t.Fatal("could not convert external service")
	}

	gotAddresses := service.Attributes.ClusterExternalAddresses.GetAddressesFor(clusterID)
	if len(gotAddresses) == 0 {
		t.Fatal("no load balancer addresses found")
	}

	for i, addr := range addresses {
		var want string
		if len(addr.IP) > 0 {
			want = addr.IP
		} else {
			want = addr.Hostname
		}
		got := gotAddresses[i]
		if got != want {
			t.Fatalf("Expected address %s but got %s", want, got)
		}
	}
}

func TestInternalTrafficPolicyServiceConversion(t *testing.T) {
	serviceName := "service1"
	namespace := "default"
	local := corev1.ServiceInternalTrafficPolicyLocal

	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					Protocol: corev1.ProtocolTCP,
				},
			},
			InternalTrafficPolicy: &local,
		},
	}

	service := ConvertService(svc, domainSuffix, clusterID, "")
	if service == nil {
		t.Fatal("could not convert service")
	}

	if !service.Attributes.NodeLocal {
		t.Fatal("not node local")
	}
}

func TestSecureNamingSAN(t *testing.T) {
	pod := &corev1.Pod{}

	pod.Annotations = make(map[string]string)

	ns := "anything"
	sa := "foo"
	pod.Namespace = ns
	pod.Spec.ServiceAccountName = sa

	san := SecureNamingSAN(pod, "td.local")

	expectedSAN := fmt.Sprintf("spiffe://td.local/ns/%v/sa/%v", ns, sa)

	if san != expectedSAN {
		t.Fatalf("SAN match failed, SAN:%v  expectedSAN:%v", san, expectedSAN)
	}
}
