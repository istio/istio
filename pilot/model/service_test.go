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
	"example-service1.default:grpc,http:a=b,c=d;e=f": {
		Hostname: "example-service1.default",
		Tags:     []Tag{{"e": "f"}, {"c": "d", "a": "b"}},
		Ports:    []*Port{{Name: "http"}, {Name: "grpc"}}},
	"my-service": {
		Hostname: "my-service",
		Ports:    []*Port{{Name: ""}}},
	"svc.ns": {
		Hostname: "svc.ns",
		Ports:    []*Port{{Name: ""}}},
	"svc::istio.io/my_tag-v1.test=my_value-v2.value": {
		Hostname: "svc",
		Tags:     []Tag{{"istio.io/my_tag-v1.test": "my_value-v2.value"}},
		Ports:    []*Port{{Name: ""}}},
	"svc:test:prod": {
		Hostname: "svc",
		Tags:     []Tag{{"prod": ""}},
		Ports:    []*Port{{Name: "test"}}},
	"svc.default.svc.cluster.local:http-test": {
		Hostname: "svc.default.svc.cluster.local",
		Ports:    []*Port{{Name: "http-test"}}},
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
		if svc1.Hostname != svc.Hostname {
			t.Errorf("svc.Hostname => Got %s, expected %s for %s", svc1.Hostname, svc.Hostname, s)
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

func TestTags(t *testing.T) {
	a := Tag{"app": "a"}
	b := Tag{"app": "b"}
	a1 := Tag{"app": "a", "prod": "env"}
	ab := TagList{a, b}
	a1b := TagList{a1, b}
	none := TagList{}

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
