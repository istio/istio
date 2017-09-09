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

package proxy_test

import (
	"reflect"
	"testing"

	"istio.io/pilot/model"
	"istio.io/pilot/proxy"
	"istio.io/pilot/test/mock"
)

func TestServiceNode(t *testing.T) {
	nodes := []struct {
		in  proxy.Node
		out string
	}{
		{
			in:  mock.HelloProxyV0,
			out: "sidecar~10.1.1.0~v0.default~default.svc.cluster.local",
		},
		{
			in: proxy.Node{
				Type:   proxy.Ingress,
				ID:     "random",
				Domain: "local",
			},
			out: "ingress~~random~local",
		},
	}

	for _, node := range nodes {
		out := node.in.ServiceNode()
		if out != node.out {
			t.Errorf("%#v.ServiceNode() => Got %s, want %s", node.in, out, node.out)
		}
		in, err := proxy.ParseServiceNode(node.out)
		if err != nil {
			t.Errorf("ParseServiceNode(%q) => Got error %v", node.out, err)
		}
		if !reflect.DeepEqual(in, node.in) {
			t.Errorf("ParseServiceNode(%q) => Got %#v, want %#v", node.out, in, node.in)
		}
	}
}

func TestParsePort(t *testing.T) {
	if port := proxy.ParsePort("localhost:3000"); port != 3000 {
		t.Errorf("ParsePort(localhost:3000) => Got %d, want 3000", port)
	}
	if port := proxy.ParsePort("localhost"); port != 0 {
		t.Errorf("ParsePort(localhost) => Got %d, want 0", port)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := proxy.DefaultProxyConfig()
	if err := model.ValidateProxyConfig(&config); err != nil {
		t.Errorf("validation of default proxy config failed with %v", err)
	}
}

func TestDefaultMeshConfig(t *testing.T) {
	mesh := proxy.DefaultMeshConfig()
	if err := model.ValidateMeshConfig(&mesh); err != nil {
		t.Errorf("validation of default mesh config failed with %v", err)
	}
}
