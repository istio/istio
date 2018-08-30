// Copyright 2018 Istio Authors
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

package v1alpha3

import (
	"reflect"
	"sort"
	"testing"

	"istio.io/istio/pilot/pkg/model"
)

func TestGenerateVirtualHostDomains(t *testing.T) {
	cases := []struct {
		name    string
		service *model.Service
		port    int
		node    *model.Proxy
		want    []string
	}{
		{
			name: "same domain",
			service: &model.Service{
				Hostname:     "foo.local.campus.net",
				MeshExternal: false,
			},
			port: 80,
			node: &model.Proxy{
				Domain: "local.campus.net",
			},
			want: []string{"foo", "foo.local", "foo.local.campus", "foo.local.campus.net",
				"foo:80", "foo.local:80", "foo.local.campus:80", "foo.local.campus.net:80"},
		},
		{
			name: "different domains with some shared dns",
			service: &model.Service{
				Hostname:     "foo.local.campus.net",
				MeshExternal: false,
			},
			port: 80,
			node: &model.Proxy{
				Domain: "remote.campus.net",
			},
			want: []string{"foo.local", "foo.local.campus", "foo.local.campus.net",
				"foo.local:80", "foo.local.campus:80", "foo.local.campus.net:80"},
		},
		{
			name: "different domains with no shared dns",
			service: &model.Service{
				Hostname:     "foo.local.campus.net",
				MeshExternal: false,
			},
			port: 80,
			node: &model.Proxy{
				Domain: "example.com",
			},
			want: []string{"foo.local.campus.net", "foo.local.campus.net:80"},
		},
	}

	for _, c := range cases {
		out := generateVirtualHostDomains(c.service, c.port, c.node)
		sort.SliceStable(c.want, func(i, j int) bool { return c.want[i] < c.want[j] })
		sort.SliceStable(out, func(i, j int) bool { return out[i] < out[j] })
		if !reflect.DeepEqual(out, c.want) {
			t.Errorf("buildVirtualHostDomains(%s): \ngot %v\n want %v", c.name, out, c.want)
		}
	}
}
