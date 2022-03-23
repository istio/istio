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

package model_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	structpb "google.golang.org/protobuf/types/known/structpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/serviceregistry/mock"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestNodeMetadata(t *testing.T) {
	tests := []struct {
		name  string
		in    model.BootstrapNodeMetadata
		out   string
		inOut model.BootstrapNodeMetadata
	}{
		{
			"empty",
			model.BootstrapNodeMetadata{},
			"{}",
			model.BootstrapNodeMetadata{NodeMetadata: model.NodeMetadata{Raw: map[string]interface{}{}}},
		},
		{
			"csvlists",
			model.BootstrapNodeMetadata{NodeMetadata: model.NodeMetadata{InstanceIPs: []string{"abc", "1.2.3.4"}}},
			`{"INSTANCE_IPS":"abc,1.2.3.4"}`,
			model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					InstanceIPs: []string{"abc", "1.2.3.4"},
					Raw: map[string]interface{}{
						"INSTANCE_IPS": "abc,1.2.3.4",
					},
				},
			},
		},
		{
			"labels",
			model.BootstrapNodeMetadata{NodeMetadata: model.NodeMetadata{Labels: map[string]string{"foo": "bar"}}},
			`{"LABELS":{"foo":"bar"}}`,
			model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					Labels: map[string]string{"foo": "bar"},
					Raw: map[string]interface{}{
						"LABELS": map[string]interface{}{
							"foo": "bar",
						},
					},
				},
			},
		},
		{
			"proxy config",
			model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					ProxyConfig: (*model.NodeMetaProxyConfig)(&meshconfig.ProxyConfig{
						ConfigPath:             "foo",
						DrainDuration:          durationpb.New(time.Second * 5),
						ControlPlaneAuthPolicy: meshconfig.AuthenticationPolicy_MUTUAL_TLS,
						EnvoyAccessLogService: &meshconfig.RemoteService{
							Address: "address",
							TlsSettings: &v1alpha3.ClientTLSSettings{
								SubjectAltNames: []string{"san"},
							},
						},
					}),
				},
			},
			// nolint: lll
			`{"PROXY_CONFIG":{"configPath":"foo","drainDuration":"5s","controlPlaneAuthPolicy":"MUTUAL_TLS","envoyAccessLogService":{"address":"address","tlsSettings":{"subjectAltNames":["san"]}}}}`,
			model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					ProxyConfig: (*model.NodeMetaProxyConfig)(&meshconfig.ProxyConfig{
						ConfigPath:             "foo",
						DrainDuration:          durationpb.New(time.Second * 5),
						ControlPlaneAuthPolicy: meshconfig.AuthenticationPolicy_MUTUAL_TLS,
						EnvoyAccessLogService: &meshconfig.RemoteService{
							Address: "address",
							TlsSettings: &v1alpha3.ClientTLSSettings{
								SubjectAltNames: []string{"san"},
							},
						},
					}),
					Raw: map[string]interface{}{
						"PROXY_CONFIG": map[string]interface{}{
							"drainDuration":          "5s",
							"configPath":             "foo",
							"controlPlaneAuthPolicy": "MUTUAL_TLS",
							"envoyAccessLogService": map[string]interface{}{
								"address": "address",
								"tlsSettings": map[string]interface{}{
									"subjectAltNames": []interface{}{"san"},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			if string(j) != tt.out {
				t.Errorf("Got json '%s', expected '%s'", string(j), tt.out)
			}
			var meta model.BootstrapNodeMetadata
			if err := json.Unmarshal(j, &meta); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			assert.Equal(t, (*meshconfig.ProxyConfig)(meta.NodeMetadata.ProxyConfig), (*meshconfig.ProxyConfig)(tt.inOut.NodeMetadata.ProxyConfig))
			// cmp cannot handle the type-alias in the metadata, so check them separately.
			meta.NodeMetadata.ProxyConfig = nil
			tt.inOut.NodeMetadata.ProxyConfig = nil
			assert.Equal(t, meta, tt.inOut)
		})
	}
}

func TestStringList(t *testing.T) {
	cases := []struct {
		in          string
		expect      model.StringList
		noRoundTrip bool
	}{
		{in: `"a,b,c"`, expect: []string{"a", "b", "c"}},
		{in: `"a"`, expect: []string{"a"}},
		{in: `""`, expect: []string{}},
		{in: `"123,@#$#,abcdef"`, expect: []string{"123", "@#$#", "abcdef"}},
		{in: `1`, expect: []string{}, noRoundTrip: true},
	}
	for _, tt := range cases {
		t.Run(tt.in, func(t *testing.T) {
			var out model.StringList
			if err := json.Unmarshal([]byte(tt.in), &out); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(out, tt.expect) {
				t.Fatalf("Expected %v, got %v", tt.expect, out)
			}
			b, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			if tt.noRoundTrip {
				return
			}
			if !reflect.DeepEqual(string(b), tt.in) {
				t.Fatalf("Expected %v, got %v", tt.in, string(b))
			}
		})
	}
}

func TestPodPortList(t *testing.T) {
	cases := []struct {
		name            string
		in              string
		expect          model.PodPortList
		expUnmarshalErr string
	}{
		{"no port", `"[]"`, model.PodPortList{}, ""},
		{"one port", `"[{\"name\":\"foo\",\"containerPort\":9080,\"protocol\":\"TCP\"}]"`, model.PodPortList{{"foo", 9080, "TCP"}}, ""},
		{
			"two ports",
			`"[{\"name\":\"foo\",\"containerPort\":9080,\"protocol\":\"TCP\"},{\"containerPort\":8888,\"protocol\":\"TCP\"}]"`,
			model.PodPortList{{"foo", 9080, "TCP"}, {ContainerPort: 8888, Protocol: "TCP"}},
			"",
		},
		{"invalid syntax", `[]`, nil, "invalid syntax"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var out model.PodPortList
			if err := json.Unmarshal([]byte(tt.in), &out); err != nil {
				if tt.expUnmarshalErr == "" {
					t.Fatal(err)
				}
				if out != nil {
					t.Fatalf("%s: Expected null unmarshal output but obtained a non-null one.", tt.name)
				}
				if err.Error() != tt.expUnmarshalErr {
					t.Fatalf("%s: Expected error: %s but got error: %s.", tt.name, tt.expUnmarshalErr, err.Error())
				}
				return
			}
			if !reflect.DeepEqual(out, tt.expect) {
				t.Fatalf("%s: Expected %v, got %v", tt.name, tt.expect, out)
			}
			b, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(string(b), tt.in) {
				t.Fatalf("%s: Expected %v, got %v", tt.name, tt.in, string(b))
			}
		})
	}
}

func TestStringBool(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		expect string
	}{
		{"1", `"1"`, `"true"`},
		{"0", `"0"`, `"false"`},
		{"false", `"false"`, `"false"`},
		{"true", `"true"`, `"true"`},
		{"invalid input", `"foo"`, ``},
		{"no quotes", `true`, ``},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var out model.StringBool
			if err := json.Unmarshal([]byte(tt.in), &out); err != nil {
				if tt.expect == "" {
					return
				}
				t.Fatal(err)
			}
			b, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(string(b), tt.expect) {
				t.Fatalf("Expected %v, got %v", tt.expect, string(b))
			}
		})
	}
}

func TestServiceNode(t *testing.T) {
	cases := []struct {
		in  *model.Proxy
		out string
	}{
		{
			in:  &mock.HelloProxyV0,
			out: "sidecar~10.1.1.0~v0.default~default.svc.cluster.local",
		},
		{
			in: &model.Proxy{
				Type:         model.Router,
				ID:           "random",
				IPAddresses:  []string{"10.3.3.3"},
				DNSDomain:    "local",
				Metadata:     &model.NodeMetadata{},
				IstioVersion: model.MaxIstioVersion,
			},
			out: "router~10.3.3.3~random~local",
		},
		{
			in: &model.Proxy{
				Type:        model.SidecarProxy,
				ID:          "random",
				IPAddresses: []string{"10.3.3.3", "10.4.4.4", "10.5.5.5", "10.6.6.6"},
				DNSDomain:   "local",
				Metadata: &model.NodeMetadata{
					InstanceIPs: []string{"10.3.3.3", "10.4.4.4", "10.5.5.5", "10.6.6.6"},
				},
				IstioVersion: model.MaxIstioVersion,
			},
			out: "sidecar~10.3.3.3~random~local",
		},
	}

	for _, node := range cases {
		out := node.in.ServiceNode()
		if out != node.out {
			t.Errorf("%#v.ServiceNode() => Got %s, want %s", node.in, out, node.out)
		}
		in, err := model.ParseServiceNodeWithMetadata(node.out, node.in.Metadata)
		if err != nil {
			t.Errorf("ParseServiceNode(%q) => Got error %v", node.out, err)
		}
		if !reflect.DeepEqual(in, node.in) {
			t.Errorf("ParseServiceNode(%q) => Got %#v, want %#v", node.out, in, node.in)
		}
	}
}

func TestParseMetadata(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]interface{}
		out      *model.Proxy
	}{
		{
			name: "Basic Case",
			out: &model.Proxy{
				Type: "sidecar", IPAddresses: []string{"1.1.1.1"}, DNSDomain: "domain", ID: "id", IstioVersion: model.MaxIstioVersion,
				Metadata: &model.NodeMetadata{Raw: map[string]interface{}{}},
			},
		},
		{
			name:     "Capture Arbitrary Metadata",
			metadata: map[string]interface{}{"foo": "bar"},
			out: &model.Proxy{
				Type: "sidecar", IPAddresses: []string{"1.1.1.1"}, DNSDomain: "domain", ID: "id", IstioVersion: model.MaxIstioVersion,
				Metadata: &model.NodeMetadata{
					Raw: map[string]interface{}{
						"foo": "bar",
					},
				},
			},
		},
		{
			name: "Capture Labels",
			metadata: map[string]interface{}{
				"LABELS": map[string]string{
					"foo": "bar",
				},
			},
			out: &model.Proxy{
				Type: "sidecar", IPAddresses: []string{"1.1.1.1"}, DNSDomain: "domain", ID: "id", IstioVersion: model.MaxIstioVersion,
				Metadata: &model.NodeMetadata{
					Raw: map[string]interface{}{
						"LABELS": map[string]interface{}{"foo": "bar"},
					},
					Labels: map[string]string{"foo": "bar"},
				},
			},
		},
		{
			name: "Capture Pod Ports",
			metadata: map[string]interface{}{
				"POD_PORTS": `[{"name":"http","containerPort":8080,"protocol":"TCP"},{"name":"grpc","containerPort":8079,"protocol":"TCP"}]`,
			},
			out: &model.Proxy{
				Type: "sidecar", IPAddresses: []string{"1.1.1.1"}, DNSDomain: "domain", ID: "id", IstioVersion: model.MaxIstioVersion,
				Metadata: &model.NodeMetadata{
					Raw: map[string]interface{}{
						"POD_PORTS": `[{"name":"http","containerPort":8080,"protocol":"TCP"},{"name":"grpc","containerPort":8079,"protocol":"TCP"}]`,
					},
					PodPorts: []model.PodPort{
						{"http", 8080, "TCP"},
						{"grpc", 8079, "TCP"},
					},
				},
			},
		},
	}

	nodeID := "sidecar~1.1.1.1~id~domain"

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := mapToStruct(tt.metadata)
			if err != nil {
				t.Fatalf("failed to setup metadata: %v", err)
			}
			parsed, err := model.ParseMetadata(meta)
			if err != nil {
				t.Fatalf("failed to parse service node: %v", err)
			}
			node, err := model.ParseServiceNodeWithMetadata(nodeID, parsed)
			if err != nil {
				t.Fatalf("failed to parse service node: %v", err)
			}
			if !reflect.DeepEqual(*tt.out.Metadata, *node.Metadata) {
				t.Errorf("Got \n%v, want \n%v", *node.Metadata, *tt.out.Metadata)
			}
		})
	}
}

func mapToStruct(msg map[string]interface{}) (*structpb.Struct, error) {
	if msg == nil {
		return &structpb.Struct{}, nil
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	pbs := &structpb.Struct{}
	if err := protomarshal.Unmarshal(b, pbs); err != nil {
		return nil, err
	}

	return pbs, nil
}

func TestParsePort(t *testing.T) {
	if port := model.ParsePort("localhost:3000"); port != 3000 {
		t.Errorf("ParsePort(localhost:3000) => Got %d, want 3000", port)
	}
	if port := model.ParsePort("localhost"); port != 0 {
		t.Errorf("ParsePort(localhost) => Got %d, want 0", port)
	}
	if port := model.ParsePort("127.0.0.1:3000"); port != 3000 {
		t.Errorf("ParsePort(127.0.0.1:3000) => Got %d, want 3000", port)
	}
	if port := model.ParsePort("127.0.0.1"); port != 0 {
		t.Errorf("ParsePort(127.0.0.1) => Got %d, want 0", port)
	}
	if port := model.ParsePort("[::1]:3000"); port != 3000 {
		t.Errorf("ParsePort([::1]:3000) => Got %d, want 3000", port)
	}
	if port := model.ParsePort("::1"); port != 0 {
		t.Errorf("ParsePort(::1) => Got %d, want 0", port)
	}
	if port := model.ParsePort("[2001:4860:0:2001::68]:3000"); port != 3000 {
		t.Errorf("ParsePort([2001:4860:0:2001::68]:3000) => Got %d, want 3000", port)
	}
	if port := model.ParsePort("2001:4860:0:2001::68"); port != 0 {
		t.Errorf("ParsePort(2001:4860:0:2001::68) => Got %d, want 0", port)
	}
}

func TestGetOrDefault(t *testing.T) {
	assert.Equal(t, "a", model.GetOrDefault("a", "b"))
	assert.Equal(t, "b", model.GetOrDefault("", "b"))
}

func TestProxyVersion_Compare(t *testing.T) {
	type fields struct {
		Major int
		Minor int
		Patch int
	}
	type args struct {
		inv *model.IstioVersion
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   int
	}{
		{
			name:   "greater major",
			fields: fields{Major: 2, Minor: 1, Patch: 1},
			args:   args{&model.IstioVersion{Major: 1, Minor: 2, Patch: 1}},
			want:   1,
		},
		{
			name:   "equal at minor",
			fields: fields{Major: 2, Minor: 1, Patch: 1},
			args:   args{&model.IstioVersion{Major: 2, Minor: 1, Patch: -1}},
			want:   0,
		},
		{
			name:   "less at patch",
			fields: fields{Major: 2, Minor: 1, Patch: 0},
			args:   args{&model.IstioVersion{Major: 2, Minor: 1, Patch: 1}},
			want:   -1,
		},
		{
			name:   "ignore minor",
			fields: fields{Major: 2, Minor: 1, Patch: 11},
			args:   args{&model.IstioVersion{Major: 2, Minor: -1, Patch: 1}},
			want:   0,
		},
		{
			name:   "ignore patch",
			fields: fields{Major: 2, Minor: 1, Patch: 11},
			args:   args{&model.IstioVersion{Major: 2, Minor: 1, Patch: -1}},
			want:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pversion := &model.IstioVersion{
				Major: tt.fields.Major,
				Minor: tt.fields.Minor,
				Patch: tt.fields.Patch,
			}
			if got := pversion.Compare(tt.args.inv); got != tt.want {
				t.Errorf("ProxyVersion.Compare() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseIstioVersion(t *testing.T) {
	type args struct {
		ver string
	}
	tests := []struct {
		name string
		args args
		want *model.IstioVersion
	}{
		{
			name: "major.minor.patch",
			args: args{ver: "1.2.3"},
			want: &model.IstioVersion{Major: 1, Minor: 2, Patch: 3},
		},
		{
			name: "major.minor",
			args: args{ver: "1.2"},
			want: &model.IstioVersion{Major: 1, Minor: 2, Patch: 65535},
		},
		{
			name: "dev",
			args: args{ver: "1.5-alpha.f70faea2aa817eeec0b08f6cc3b5078e5dcf3beb"},
			want: &model.IstioVersion{Major: 1, Minor: 5, Patch: 65535},
		},
		{
			name: "release-major.minor-date",
			args: args{ver: "release-1.2-123214234"},
			want: &model.IstioVersion{Major: 1, Minor: 2, Patch: 65535},
		},
		{
			name: "master-date",
			args: args{ver: "master-123214234"},
			want: model.MaxIstioVersion,
		},
		{
			name: "master-sha",
			args: args{ver: "master-0b94e017f5b6c7c4598a4da42ea9d45eeb099e5f"},
			want: model.MaxIstioVersion,
		},
		{
			name: "junk-major.minor.patch",
			args: args{ver: "junk-1.2.3214234"},
			want: model.MaxIstioVersion,
		},
		{
			name: "junk-garbage",
			args: args{ver: "junk-garbage"},
			want: model.MaxIstioVersion,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.ParseIstioVersion(tt.args.ver); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseIstioVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetServiceInstances(t *testing.T) {
	tnow := time.Now()
	instances := []*model.ServiceInstance{
		{
			Service: &model.Service{
				CreationTime: tnow.Add(1 * time.Second),
				Hostname:     host.Name("test1.com"),
			},
		},
		{
			Service: &model.Service{
				CreationTime: tnow,
				Hostname:     host.Name("test3.com"),
			},
		},
		{
			Service: &model.Service{
				CreationTime: tnow,
				Hostname:     host.Name("test2.com"),
			},
		},
	}

	serviceDiscovery := memory.NewServiceDiscovery()
	serviceDiscovery.WantGetProxyServiceInstances = instances

	env := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
	}

	proxy := &model.Proxy{}
	proxy.SetServiceInstances(env)

	assert.Equal(t, len(proxy.ServiceInstances), 3)
	assert.Equal(t, proxy.ServiceInstances[0].Service.Hostname, host.Name("test2.com"))
	assert.Equal(t, proxy.ServiceInstances[1].Service.Hostname, host.Name("test3.com"))
	assert.Equal(t, proxy.ServiceInstances[2].Service.Hostname, host.Name("test1.com"))
}

func TestGlobalUnicastIP(t *testing.T) {
	cases := []struct {
		name   string
		in     []string
		expect string
	}{
		{
			name:   "single IPv4 (k8s)",
			in:     []string{"10.0.4.16"},
			expect: "10.0.4.16",
		},
		{
			name:   "single IPv6 (k8s)",
			in:     []string{"fc00:f853:ccd:e793::1"},
			expect: "fc00:f853:ccd:e793::1",
		},
		{
			name:   "multi IPv4 [1st] (VM)",
			in:     []string{"10.128.0.51", "fc00:f853:ccd:e793::1", "172.17.0.1", "fe80::42:35ff:fec1:7436", "fe80::345d:33ff:fe54:5c8e"},
			expect: "10.128.0.51",
		},
		{
			name:   "multi IPv6 [1st] (VM)",
			in:     []string{"fc00:f853:ccd:e793::1", "172.17.0.1", "fe80::42:35ff:fec1:7436", "10.128.0.51", "fe80::345d:33ff:fe54:5c8e"},
			expect: "fc00:f853:ccd:e793::1",
		},
		{
			name:   "multi IPv4 [2nd] (VM)",
			in:     []string{"127.0.0.1", "10.128.0.51", "fc00:f853:ccd:e793::1", "172.17.0.1", "fe80::42:35ff:fec1:7436", "fe80::345d:33ff:fe54:5c8e"},
			expect: "10.128.0.51",
		},
		{
			name:   "multi IPv6 [2nd] (VM)",
			in:     []string{"fe80::42:35ff:fec1:7436", "fc00:f853:ccd:e793::1", "172.17.0.1", "10.128.0.51", "fe80::345d:33ff:fe54:5c8e"},
			expect: "fc00:f853:ccd:e793::1",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var node model.Proxy
			node.IPAddresses = tt.in
			node.DiscoverIPVersions()
			if got := node.GlobalUnicastIP; got != tt.expect {
				t.Errorf("GlobalUnicastIP = %v, want %v", got, tt.expect)
			}
		})
	}
}
