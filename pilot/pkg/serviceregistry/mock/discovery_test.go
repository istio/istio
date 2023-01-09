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

package mock

import (
	"testing"
)

func TestMakeIP(t *testing.T) {
	HelloService := MakeService(ServiceArgs{
		Hostname:        "hello.default.svc.cluster.local",
		Address:         "10.1.0.0",
		ServiceAccounts: []string{},
		ClusterID:       "cluster-1",
	})
	HelloInstanceV0 := MakeIP(HelloService, 0)

	if HelloInstanceV0 != "10.1.1.0" {
		t.Fatalf("MakeIP() can not handle ip4 address.")
	}

	HelloService1 := MakeService(ServiceArgs{
		Hostname:        "hello.default.svc.cluster.local",
		Address:         "asa",
		ServiceAccounts: []string{},
		ClusterID:       "cluster-1",
	})
	HelloInstanceV1 := MakeIP(HelloService1, 0)
	if HelloInstanceV1 != "" {
		t.Fatalf("MakeIP() can not handle string not the ip address.")
	}

	HelloService2 := MakeService(ServiceArgs{
		Hostname:        "hello.default.svc.cluster.local",
		Address:         "0:0:0:0:0:ffff:192.1.56.10",
		ServiceAccounts: []string{},
		ClusterID:       "cluster-1",
	})
	HelloInstanceV2 := MakeIP(HelloService2, 0)
	if HelloInstanceV2 != "192.1.1.0" {
		t.Fatalf("MakeIP() can not handle ip6 address.")
	}
}
