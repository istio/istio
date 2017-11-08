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

package mock

import (
	"testing"

	proxyconfig "istio.io/api/proxy/v1/config"
)

func TestMockServices(t *testing.T) {
	svcs, err := Discovery.Services()
	if err != nil {
		t.Errorf("Discovery.Services encountered error: %v", err)
	}
	for _, svc := range svcs {
		if err := svc.Validate(); err != nil {
			t.Errorf("%v.Validate() => Got %v", svc, err)
		}
		for _, port := range svc.Ports {
			if port.AuthenticationPolicy != proxyconfig.AuthenticationPolicy_INHERIT {
				t.Errorf("Default port authentication policy must be INHERIT. Got %v", *port)
			}
		}
		instances, err := Discovery.Instances(svc.Hostname, svc.Ports.GetNames(), nil)
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
