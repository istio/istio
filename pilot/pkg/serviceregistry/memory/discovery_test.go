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

package memory

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
)

func TestMemoryServices(t *testing.T) {
	helloService := MakeService("hello.default.svc.cluster.local", "10.1.0.0")
	worldService := MakeService("world.default.svc.cluster.local", "10.2.0.0")
	extHTTPService := MakeExternalHTTPService("httpbin.default.svc.cluster.local",
		"httpbin.org", "")
	extHTTPSService := MakeExternalHTTPSService("httpsbin.default.svc.cluster.local",
		"httpbin.org", "")

	discovery := &ServiceDiscovery{
		services: map[model.Hostname]*model.Service{
			helloService.Hostname:   helloService,
			worldService.Hostname:   worldService,
			extHTTPService.Hostname: extHTTPService,
			// TODO external https is not currently supported - this service
			// should NOT be in any of the .golden json files
			extHTTPSService.Hostname: extHTTPSService,
		},
		versions: 2,
	}

	svcs, err := discovery.Services()
	if err != nil {
		t.Errorf("Discovery.Services encountered error: %v", err)
	}
	for _, svc := range svcs {
		if err := svc.Validate(); err != nil {
			t.Errorf("%v.Validate() => Got %v", svc, err)
		}
		instances, err := discovery.Instances(svc.Hostname, svc.Ports.GetNames(), nil)
		if err != nil {
			t.Errorf("Discovery.Instances encountered error: %v", err)
		}
		if svc.External() {
			if len(instances) > 0 {
				t.Errorf("Discovery.Instances => Got %d, want 0 for external service", len(instances))
			}
		} else {
			if len(instances) == 0 {
				t.Errorf("Discovery.Instances => Got %d, want positive", len(instances))
			}
			for _, instance := range instances {
				if err := instance.Validate(); err != nil {
					t.Errorf("%v.Validate() => Got %v", instance, err)
				}
			}
		}
	}
}
