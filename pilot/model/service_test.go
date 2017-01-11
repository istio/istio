// Copyright 2017 Google Inc.
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

import "testing"

var validServices = map[string]Service{
	"example-service1.default:grpc,http:v1,v2,v3": Service{
		Name:      "example-service1",
		Namespace: "default",
		Tags:      []string{"v2", "v3", "v1"},
		Ports:     []Port{Port{Name: "http"}, Port{Name: "grpc"}}},
	"my-service":    Service{Name: "my-service", Ports: []Port{Port{Name: ""}}},
	"svc.ns":        Service{Name: "svc", Ports: []Port{Port{Name: ""}}, Namespace: "ns"},
	"svc::v1-test":  Service{Name: "svc", Ports: []Port{Port{Name: ""}}, Tags: []string{"v1-test"}},
	"svc:http-test": Service{Name: "svc", Ports: []Port{Port{Name: "http-test"}}},
}

func TestServiceString(t *testing.T) {
	for s, svc := range validServices {
		if err := svc.Validate(); err != nil {
			t.Errorf("Valid service failed validation: %v,  %#v", err, svc)
		}
		s1 := svc.String()
		if s1 != s {
			t.Errorf("svc.String() => Got %s, expected %s", s1, s)
		}
		svc1 := ParseServiceString(s)
		if svc1.Name != svc.Name {
			t.Errorf("svc.Name => Got %s, expected %s for %s", svc1.Name, svc.Name, s)
		}
		if svc1.Namespace != svc.Namespace {
			t.Errorf("svc.Name => Got %s, expected %s for %s", svc1.Namespace, svc.Namespace, s)
		}
		if !compare(svc1.Tags, svc.Tags) {
			t.Errorf("svc.Tags => Got %#v, expected %#v for %s", svc1.Tags, svc.Tags, s)
		}
		if len(svc1.Ports) != len(svc.Ports) {
			t.Errorf("svc.Ports => Got %#v, expected %#v for %s", svc1.Ports, svc.Ports, s)
		}
	}
}

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
