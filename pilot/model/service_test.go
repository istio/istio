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

import "testing"

var validServiceKeys = map[string]struct {
	service Service
	tags    TagList
}{
	"example-service1.default:grpc,http:a=b,c=d;e=f": {
		service: Service{
			Hostname: "example-service1.default",
			Ports:    []*Port{{Name: "http"}, {Name: "grpc"}}},
		tags: TagList{{"e": "f"}, {"c": "d", "a": "b"}}},
	"my-service": {
		service: Service{
			Hostname: "my-service",
			Ports:    []*Port{{Name: ""}}}},
	"svc.ns": {
		service: Service{
			Hostname: "svc.ns",
			Ports:    []*Port{{Name: ""}}}},
	"svc::istio.io/my_tag-v1.test=my_value-v2.value": {
		service: Service{
			Hostname: "svc",
			Ports:    []*Port{{Name: ""}}},
		tags: TagList{{"istio.io/my_tag-v1.test": "my_value-v2.value"}}},
	"svc:test:prod": {
		service: Service{
			Hostname: "svc",
			Ports:    []*Port{{Name: "test"}}},
		tags: TagList{{"prod": ""}}},
	"svc.default.svc.cluster.local:http-test": {
		service: Service{
			Hostname: "svc.default.svc.cluster.local",
			Ports:    []*Port{{Name: "http-test"}}}},
}

func TestServiceString(t *testing.T) {
	for s, svc := range validServiceKeys {
		if err := svc.service.Validate(); err != nil {
			t.Errorf("Valid service failed validation: %v,  %#v", err, svc.service)
		}
		s1 := ServiceKey(svc.service.Hostname, svc.service.Ports, svc.tags)
		if s1 != s {
			t.Errorf("ServiceKey => Got %s, expected %s", s1, s)
		}
		hostname, ports, tags := ParseServiceKey(s)
		if hostname != svc.service.Hostname {
			t.Errorf("ParseServiceKey => Got %s, expected %s for %s", hostname, svc.service.Hostname, s)
		}
		if !compareTags(tags, svc.tags) {
			t.Errorf("ParseServiceKey => Got %#v, expected %#v for %s", tags, svc.tags, s)
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

func TestTags(t *testing.T) {
	a := Tag{"app": "a"}
	b := Tag{"app": "b"}
	a1 := Tag{"app": "a", "prod": "env"}
	ab := TagList{a, b}
	a1b := TagList{a1, b}
	none := TagList{}

	// equivalent to empty tag list
	singleton := TagList{nil}

	var empty Tag
	if !empty.SubsetOf(a) {
		t.Errorf("nil.SubsetOf({a}) => Got false")
	}

	if a.SubsetOf(empty) {
		t.Errorf("{a}.SubsetOf(nil) => Got true")
	}

	matching := []struct {
		tag  Tag
		list TagList
	}{
		{a, ab},
		{b, ab},
		{a, none},
		{a, nil},
		{a, singleton},
		{a1, ab},
		{b, a1b},
	}

	if (TagList{a}).HasSubsetOf(b) {
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
