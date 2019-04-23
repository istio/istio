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

package model

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

var validServiceKeys = map[string]struct {
	service *Service
	labels  LabelsCollection
}{
	"example-service1.default|grpc,http|a=b,c=d;e=f": {
		service: &Service{
			Hostname: "example-service1.default",
			Ports:    []*Port{{Name: "http", Port: 80}, {Name: "grpc", Port: 90}}},
		labels: LabelsCollection{{"e": "f"}, {"c": "d", "a": "b"}}},
	"my-service": {
		service: &Service{
			Hostname: "my-service",
			Ports:    []*Port{{Name: "", Port: 80}}}},
	"svc.ns": {
		service: &Service{
			Hostname: "svc.ns",
			Ports:    []*Port{{Name: "", Port: 80}}}},
	"svc||istio.io/my_tag-v1.test=my_value-v2.value": {
		service: &Service{
			Hostname: "svc",
			Ports:    []*Port{{Name: "", Port: 80}}},
		labels: LabelsCollection{{"istio.io/my_tag-v1.test": "my_value-v2.value"}}},
	"svc|test|prod": {
		service: &Service{
			Hostname: "svc",
			Ports:    []*Port{{Name: "test", Port: 80}}},
		labels: LabelsCollection{{"prod": ""}}},
	"svc.default.svc.cluster.local|http-test": {
		service: &Service{
			Hostname: "svc.default.svc.cluster.local",
			Ports:    []*Port{{Name: "http-test", Port: 80}}}},
}

func TestServiceString(t *testing.T) {
	for s, svc := range validServiceKeys {
		if err := svc.service.Validate(); err != nil {
			t.Errorf("Valid service failed validation: %v,  %#v", err, svc.service)
		}
		s1 := ServiceKey(svc.service.Hostname, svc.service.Ports, svc.labels)
		if s1 != s {
			t.Errorf("ServiceKey => Got %s, expected %s", s1, s)
		}
		hostname, ports, labels := ParseServiceKey(s)
		if hostname != svc.service.Hostname {
			t.Errorf("ParseServiceKey => Got %s, expected %s for %s", hostname, svc.service.Hostname, s)
		}
		if !compareLabels(labels, svc.labels) {
			t.Errorf("ParseServiceKey => Got %#v, expected %#v for %s", labels, svc.labels, s)
		}
		if len(ports) != len(svc.service.Ports) {
			t.Errorf("ParseServiceKey => Got %#v, expected %#v for %s", ports, svc.service.Ports, s)
		}
	}
}

// compare two slices of strings as sets
func compare(a, b []string) bool {
	ma := make(map[string]bool)
	mb := make(map[string]bool)
	for _, i := range a {
		ma[i] = true
	}
	for _, i := range b {
		mb[i] = true
	}
	for key := range ma {
		if !mb[key] {
			return false
		}
	}
	for key := range mb {
		if !ma[key] {
			return false
		}
	}

	return true
}

// compareLabels compares sets of labels
func compareLabels(a, b []Labels) bool {
	var as, bs []string
	for _, i := range a {
		as = append(as, i.String())
	}
	for _, j := range b {
		bs = append(bs, j.String())
	}
	return compare(as, bs)
}

func TestLabels(t *testing.T) {
	a := Labels{"app": "a"}
	b := Labels{"app": "b"}
	a1 := Labels{"app": "a", "prod": "env"}
	ab := LabelsCollection{a, b}
	a1b := LabelsCollection{a1, b}
	none := LabelsCollection{}

	// equivalent to empty tag list
	singleton := LabelsCollection{nil}

	var empty Labels
	if !empty.SubsetOf(a) {
		t.Errorf("nil.SubsetOf({a}) => Got false")
	}

	if a.SubsetOf(empty) {
		t.Errorf("{a}.SubsetOf(nil) => Got true")
	}

	matching := []struct {
		tag  Labels
		list LabelsCollection
	}{
		{a, ab},
		{b, ab},
		{a, none},
		{a, nil},
		{a, singleton},
		{a1, ab},
		{b, a1b},
	}

	if (LabelsCollection{a}).HasSubsetOf(b) {
		t.Errorf("{a}.HasSubsetOf(b) => Got true")
	}

	if a1.SubsetOf(a) {
		t.Errorf("%v.SubsetOf(%v) => Got true", a1, a)
	}

	for _, pair := range matching {
		if !pair.list.HasSubsetOf(pair.tag) {
			t.Errorf("%v.HasSubsetOf(%v) => Got false", pair.list, pair.tag)
		}
	}
}

func TestHTTPProtocol(t *testing.T) {
	if ProtocolUDP.IsHTTP() {
		t.Errorf("UDP is not HTTP protocol")
	}
	if !ProtocolGRPC.IsHTTP() {
		t.Errorf("gRPC is HTTP protocol")
	}
}

func TestGetByPort(t *testing.T) {
	ports := PortList{{
		Name: "http",
		Port: 80,
	}}

	if port, exists := ports.GetByPort(80); !exists || port == nil || port.Name != "http" {
		t.Errorf("GetByPort(80) => want http but got %v, %t", port, exists)
	}
	if port, exists := ports.GetByPort(88); exists || port != nil {
		t.Errorf("GetByPort(88) => want none but got %v, %t", port, exists)
	}
}

func TestParseProtocol(t *testing.T) {
	var testPairs = []struct {
		name string
		out  Protocol
	}{
		{"tcp", ProtocolTCP},
		{"http", ProtocolHTTP},
		{"HTTP", ProtocolHTTP},
		{"Http", ProtocolHTTP},
		{"https", ProtocolHTTPS},
		{"http2", ProtocolHTTP2},
		{"grpc", ProtocolGRPC},
		{"grpc-web", ProtocolGRPCWeb},
		{"gRPC-Web", ProtocolGRPCWeb},
		{"grpc-Web", ProtocolGRPCWeb},
		{"udp", ProtocolUDP},
		{"Mongo", ProtocolMongo},
		{"mongo", ProtocolMongo},
		{"MONGO", ProtocolMongo},
		{"Redis", ProtocolRedis},
		{"redis", ProtocolRedis},
		{"REDIS", ProtocolRedis},
		{"Mysql", ProtocolMySQL},
		{"mysql", ProtocolMySQL},
		{"MYSQL", ProtocolMySQL},
		{"MySQL", ProtocolMySQL},
		{"", ProtocolUnsupported},
		{"SMTP", ProtocolUnsupported},
	}

	for _, testPair := range testPairs {
		out := ParseProtocol(testPair.name)
		if out != testPair.out {
			t.Errorf("ParseProtocol(%q) => %q, want %q", testPair.name, out, testPair.out)
		}
	}
}

func TestHostnameMatches(t *testing.T) {
	tests := []struct {
		name string
		a, b Hostname
		out  bool
	}{
		{"empty", "", "", true},
		{"first empty", "", "foo.com", false},
		{"second empty", "foo.com", "", false},

		{"non-wildcard domain",
			"foo.com", "foo.com", true},
		{"non-wildcard domain",
			"bar.com", "foo.com", false},
		{"non-wildcard domain - order doesn't matter",
			"foo.com", "bar.com", false},

		{"domain does not match subdomain",
			"bar.foo.com", "foo.com", false},
		{"domain does not match subdomain - order doesn't matter",
			"foo.com", "bar.foo.com", false},

		{"wildcard matches subdomains",
			"*.com", "foo.com", true},
		{"wildcard matches subdomains",
			"*.com", "bar.com", true},
		{"wildcard matches subdomains",
			"*.foo.com", "bar.foo.com", true},

		{"wildcard matches anything", "*", "foo.com", true},
		{"wildcard matches anything", "*", "*.com", true},
		{"wildcard matches anything", "*", "com", true},
		{"wildcard matches anything", "*", "*", true},
		{"wildcard matches anything", "*", "", true},

		{"wildcarded domain matches wildcarded subdomain", "*.com", "*.foo.com", true},
		{"wildcarded sub-domain does not match domain", "foo.com", "*.foo.com", false},
		{"wildcarded sub-domain does not match domain - order doesn't matter", "*.foo.com", "foo.com", false},

		{"long wildcard does not match short host", "*.foo.bar.baz", "baz", false},
		{"long wildcard does not match short host - order doesn't matter", "baz", "*.foo.bar.baz", false},
		{"long wildcard matches short wildcard", "*.foo.bar.baz", "*.baz", true},
		{"long name matches short wildcard", "foo.bar.baz", "*.baz", true},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if tt.out != tt.a.Matches(tt.b) {
				t.Fatalf("%q.Matches(%q) = %t wanted %t", tt.a, tt.b, !tt.out, tt.out)
			}
		})
	}
}

func TestHostnameSubsetOf(t *testing.T) {
	tests := []struct {
		name string
		a, b Hostname
		out  bool
	}{
		{"empty", "", "", true},
		{"first empty", "", "foo.com", false},
		{"second empty", "foo.com", "", false},

		{"non-wildcard domain",
			"foo.com", "foo.com", true},
		{"non-wildcard domain",
			"bar.com", "foo.com", false},
		{"non-wildcard domain - order doesn't matter",
			"foo.com", "bar.com", false},

		{"domain does not match subdomain",
			"bar.foo.com", "foo.com", false},
		{"domain does not match subdomain - order doesn't matter",
			"foo.com", "bar.foo.com", false},

		{"wildcard matches subdomains",
			"foo.com", "*.com", true},
		{"wildcard matches subdomains",
			"bar.com", "*.com", true},
		{"wildcard matches subdomains",
			"bar.foo.com", "*.foo.com", true},

		{"wildcard matches anything", "foo.com", "*", true},
		{"wildcard matches anything", "*.com", "*", true},
		{"wildcard matches anything", "com", "*", true},
		{"wildcard matches anything", "*", "*", true},
		{"wildcard matches anything", "", "*", true},

		{"wildcarded domain matches wildcarded subdomain", "*.foo.com", "*.com", true},
		{"wildcarded sub-domain does not match domain", "*.foo.com", "foo.com", false},

		{"long wildcard does not match short host", "*.foo.bar.baz", "baz", false},
		{"long name matches short wildcard", "foo.bar.baz", "*.baz", true},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if tt.out != tt.a.SubsetOf(tt.b) {
				t.Fatalf("%q.SubsetOf(%q) = %t wanted %t", tt.a, tt.b, !tt.out, tt.out)
			}
		})
	}
}

func BenchmarkMatch(b *testing.B) {
	tests := []struct {
		a, z    Hostname
		matches bool
	}{
		{"foo.com", "foo.com", true},
		{"*.com", "foo.com", true},
		{"*.foo.com", "bar.foo.com", true},
		{"*", "foo.com", true},
		{"*", "*.com", true},
		{"*", "", true},
		{"*.com", "*.foo.com", true},
		{"foo.com", "*.foo.com", false},
		{"*.foo.bar.baz", "baz", false},
	}
	for n := 0; n < b.N; n++ {
		for _, test := range tests {
			doesMatch := test.a.Matches(test.z)
			if doesMatch != test.matches {
				b.Fatalf("does not match")
			}
		}
	}
}

func TestHostnamesIntersection(t *testing.T) {
	tests := []struct {
		a, b, intersection Hostnames
	}{
		{
			Hostnames{"foo,com"},
			Hostnames{"bar.com"},
			Hostnames{},
		},
		{
			Hostnames{"foo.com", "bar.com"},
			Hostnames{"bar.com"},
			Hostnames{"bar.com"},
		},
		{
			Hostnames{"foo.com", "bar.com"},
			Hostnames{"*.com"},
			Hostnames{"foo.com", "bar.com"},
		},
		{
			Hostnames{"*.com"},
			Hostnames{"foo.com", "bar.com"},
			Hostnames{"foo.com", "bar.com"},
		},
		{
			Hostnames{"foo.com", "*.net"},
			Hostnames{"*.com", "bar.net"},
			Hostnames{"foo.com", "bar.net"},
		},
		{
			Hostnames{"foo.com", "*.net"},
			Hostnames{"*.bar.net"},
			Hostnames{"*.bar.net"},
		},
		{
			Hostnames{"foo.com", "bar.net"},
			Hostnames{"*"},
			Hostnames{"foo.com", "bar.net"},
		},
		{
			Hostnames{"foo.com"},
			Hostnames{},
			Hostnames{},
		},
		{
			Hostnames{},
			Hostnames{"bar.com"},
			Hostnames{},
		},
		{
			Hostnames{"*", "foo.com"},
			Hostnames{"foo.com"},
			Hostnames{"foo.com"},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			result := tt.a.Intersection(tt.b)
			if !reflect.DeepEqual(result, tt.intersection) {
				t.Fatalf("%v.Intersection(%v) = %v, want %v", tt.a, tt.b, result, tt.intersection)
			}
		})
	}
}

func TestHostnamesForNamespace(t *testing.T) {
	tests := []struct {
		hosts     []string
		namespace string
		want      Hostnames
	}{
		{
			[]string{"ns1/foo.com", "ns2/bar.com"},
			"ns1",
			Hostnames{"foo.com"},
		},
		{
			[]string{"ns1/foo.com", "ns2/bar.com"},
			"ns3",
			Hostnames{},
		},
		{
			[]string{"ns1/foo.com", "*/bar.com"},
			"ns1",
			Hostnames{"foo.com", "bar.com"},
		},
		{
			[]string{"ns1/foo.com", "*/bar.com"},
			"ns3",
			Hostnames{"bar.com"},
		},
		{
			[]string{"foo.com", "ns2/bar.com"},
			"ns2",
			Hostnames{"foo.com", "bar.com"},
		},
		{
			[]string{"foo.com", "ns2/bar.com"},
			"ns3",
			Hostnames{"foo.com"},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			result := HostnamesForNamespace(tt.hosts, tt.namespace)
			if !reflect.DeepEqual(result, tt.want) {
				t.Fatalf("HostnamesForNamespace(%v, %v) = %v, want %v", tt.hosts, tt.namespace, result, tt.want)
			}
		})
	}
}

func TestHostnamesSortOrder(t *testing.T) {
	tests := []struct {
		in, want Hostnames
	}{
		// Prove we sort alphabetically:
		{
			Hostnames{"b", "a"},
			Hostnames{"a", "b"},
		},
		{
			Hostnames{"bb", "cc", "aa"},
			Hostnames{"aa", "bb", "cc"},
		},
		// Prove we sort longest first, alphabetically:
		{
			Hostnames{"b", "a", "aa"},
			Hostnames{"aa", "a", "b"},
		},
		{
			Hostnames{"foo.com", "bar.com", "foo.bar.com"},
			Hostnames{"foo.bar.com", "bar.com", "foo.com"},
		},
		// We sort wildcards last, always
		{
			Hostnames{"a", "*", "z"},
			Hostnames{"a", "z", "*"},
		},
		{
			Hostnames{"foo.com", "bar.com", "*.com"},
			Hostnames{"bar.com", "foo.com", "*.com"},
		},
		{
			Hostnames{"foo.com", "bar.com", "*.com", "*.foo.com", "*", "baz.bar.com"},
			Hostnames{"baz.bar.com", "bar.com", "foo.com", "*.foo.com", "*.com", "*"},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			// Save a copy to report errors with
			tmp := make(Hostnames, len(tt.in))
			copy(tmp, tt.in)

			sort.Sort(tt.in)
			if !reflect.DeepEqual(tt.in, tt.want) {
				t.Fatalf("sort.Sort(%v) = %v, want %v", tmp, tt.in, tt.want)
			}
		})
	}
}

func BenchmarkSort(b *testing.B) {
	unsorted := Hostnames{"foo.com", "bar.com", "*.com", "*.foo.com", "*", "baz.bar.com"}

	for n := 0; n < b.N; n++ {
		given := make(Hostnames, len(unsorted))
		copy(given, unsorted)
		sort.Sort(given)
	}
}

func TestIsValidSubsetKey(t *testing.T) {
	cases := []struct {
		subsetkey string
		expectErr bool
	}{
		{
			subsetkey: "outbound|80|subset|hostname",
			expectErr: false,
		},
		{
			subsetkey: "outbound|80||hostname",
			expectErr: false,
		},
		{
			subsetkey: "outbound|80|subset||hostname",
			expectErr: true,
		},
		{
			subsetkey: "",
			expectErr: true,
		},
	}

	for _, c := range cases {
		err := IsValidSubsetKey(c.subsetkey)
		if !err != c.expectErr {
			t.Errorf("got %v but want %v\n", err, c.expectErr)
		}
	}
}

func TestGetLocality(t *testing.T) {
	cases := []struct {
		name     string
		instance ServiceInstance
		expected string
	}{
		{
			name: "endpoint with locality is overridden by label",
			instance: ServiceInstance{
				Endpoint: NetworkEndpoint{
					Locality: "region/zone/subzone-1",
				},
				Labels: Labels{
					LocalityLabel: "region/zone/subzone-2",
				},
			},
			expected: "region/zone/subzone-2",
		},
		{
			name: "endpoint without label, use registry locality",
			instance: ServiceInstance{
				Endpoint: NetworkEndpoint{
					Locality: "region/zone/subzone-1",
				},
				Labels: Labels{
					LocalityLabel: "",
				},
			},
			expected: "region/zone/subzone-1",
		},
		{
			name: "istio-locality label with k8s label separator",
			instance: ServiceInstance{
				Endpoint: NetworkEndpoint{
					Locality: "",
				},
				Labels: Labels{
					LocalityLabel: "region" + k8sSeparator + "zone" + k8sSeparator + "subzone-2",
				},
			},
			expected: "region/zone/subzone-2",
		},
		{
			name: "istio-locality label with both k8s label separators and slashes",
			instance: ServiceInstance{
				Endpoint: NetworkEndpoint{
					Locality: "",
				},
				Labels: Labels{
					LocalityLabel: "region/zone/subzone.2",
				},
			},
			expected: "region/zone/subzone.2",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := testCase.instance.GetLocality()
			if got != testCase.expected {
				t.Errorf("expected locality %s, but got %s", testCase.expected, got)
			}
		})
	}
}
