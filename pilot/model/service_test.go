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
	"example-service1.default:grpc,http:a=b,c=d;e=f": Service{
		Name:      "example-service1",
		Namespace: "default",
		Tags:      []Tag{{"e": "f"}, {"c": "d", "a": "b"}},
		Ports:     []Port{Port{Name: "http"}, Port{Name: "grpc"}}},
	"my-service": Service{
		Name:  "my-service",
		Ports: []Port{Port{Name: ""}}},
	"svc.ns": Service{
		Name:      "svc",
		Namespace: "ns",
		Ports:     []Port{Port{Name: ""}}},
	"svc::istio.io/my_tag-v1.test=my_value-v2.value": Service{
		Name:  "svc",
		Tags:  []Tag{{"istio.io/my_tag-v1.test": "my_value-v2.value"}},
		Ports: []Port{Port{Name: ""}}},
	"svc:test:prod": Service{
		Name:  "svc",
		Tags:  []Tag{{"prod": ""}},
		Ports: []Port{Port{Name: "test"}}},
	"svc:http-test": Service{
		Name:  "svc",
		Ports: []Port{Port{Name: "http-test"}}},
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
		if !compareTags(svc1.Tags, svc.Tags) {
			t.Errorf("svc.Tags => Got %#v, expected %#v for %s", svc1.Tags, svc.Tags, s)
		}
		if len(svc1.Ports) != len(svc.Ports) {
			t.Errorf("svc.Ports => Got %#v, expected %#v for %s", svc1.Ports, svc.Ports, s)
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

// compareTags compares sets of tags
func compareTags(a, b []Tag) bool {
	var as, bs []string
	for _, i := range a {
		as = append(as, i.String())
	}
	for _, j := range b {
		bs = append(bs, j.String())
	}
	return compare(as, bs)
}
