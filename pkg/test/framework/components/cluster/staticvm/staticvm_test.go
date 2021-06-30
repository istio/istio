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

package staticvm

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestBuild(t *testing.T) {
	cfg := cluster.Config{}
	if err := yaml.Unmarshal([]byte(`
kind: StaticVM
clusterName: static-vms
primaryClusterName: istio-testing
meta:
  deployments:
  - service: vm
    namespace: echo
    instances:
    - ip: 172.17.0.4
      instanceIP: 10.0.0.1
`), &cfg); err != nil {
		t.Fatal(err)
	}
	got, err := build(cfg, cluster.Topology{})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(got.(*vmcluster).vms, []echo.Config{{
		Service:         "vm",
		Namespace:       namespace.Static("echo"),
		StaticAddresses: []string{"172.17.0.4:10.0.0.1"},
	}}); diff != "" {
		t.Fatal(diff)
	}
}

func TestVmcluster_CanDeploy(t *testing.T) {
	aSvc := "a"
	echoNS := namespace.Static("echo")
	echoGenNS := namespace.Static("echo-1234")
	ips := []string{"1.2.3.4"}
	vms := vmcluster{vms: []echo.Config{{
		Service: aSvc, Namespace: echoNS,
		StaticAddresses: ips,
	}}}

	for name, tc := range map[string]struct {
		given  echo.Config
		want   echo.Config
		wantOk bool
	}{
		"match": {
			given:  echo.Config{DeployAsVM: true, Service: aSvc, Namespace: echoGenNS, Ports: []echo.Port{{Name: "grpc"}}},
			want:   echo.Config{DeployAsVM: true, Service: aSvc, Namespace: echoGenNS, Ports: []echo.Port{{Name: "grpc"}}, StaticAddresses: ips},
			wantOk: true,
		},
		"non vm": {
			given: echo.Config{Service: aSvc, Namespace: echoGenNS, Ports: []echo.Port{{Name: "grpc"}}},
		},
		"namespace mismatch": {
			given: echo.Config{DeployAsVM: true, Service: aSvc, Namespace: namespace.Static("other"), Ports: []echo.Port{{Name: "grpc"}}},
		},
		"service mismatch": {
			given: echo.Config{DeployAsVM: true, Service: "b", Namespace: echoNS, Ports: []echo.Port{{Name: "grpc"}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := vms.CanDeploy(tc.given)
			if ok != tc.wantOk {
				t.Errorf("got %v but wanted %v", ok, tc.wantOk)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
}
