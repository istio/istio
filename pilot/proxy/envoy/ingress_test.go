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

package envoy

import (
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"

	"istio.io/istio/pilot/model"
)

func addIngressRoutes(r model.ConfigStore, t *testing.T) {
	addConfig(r, ingressRouteRule1, t)
	addConfig(r, ingressRouteRule2, t)
}

func TestRouteCombination(t *testing.T) {
	path1 := &HTTPRoute{Path: "/xyz"}
	path2 := &HTTPRoute{Path: "/xy"}
	path3 := &HTTPRoute{Path: "/z"}
	prefix1 := &HTTPRoute{Prefix: "/xyz"}
	prefix2 := &HTTPRoute{Prefix: "/x"}
	prefix3 := &HTTPRoute{Prefix: "/z"}

	testCases := []struct {
		a    *HTTPRoute
		b    *HTTPRoute
		want *HTTPRoute
	}{
		{path1, path1, path1},
		{prefix1, prefix1, prefix1},
		{path1, path2, nil},
		{path1, path3, nil},
		{prefix1, prefix2, prefix1},
		{prefix1, prefix3, nil},
		{prefix2, prefix3, nil},
		{path1, prefix1, path1},
		{path1, prefix2, path1},
		{path1, prefix3, nil},
		{path2, prefix1, nil},
		{path2, prefix2, path2},
		{path2, prefix3, nil},
		{path3, prefix3, path3},
		{path3, prefix1, nil},
		{path3, prefix2, nil},
		{path3, prefix3, path3},
	}

	for _, test := range testCases {
		a := *test.a
		got := a.CombinePathPrefix(test.b.Path, test.b.Prefix)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s.CombinePathPrefix(%s) => got %v, want %v", spew.Sdump(test.a), spew.Sdump(test.b), got, test.want)
		}
		b := *test.b
		got = b.CombinePathPrefix(test.a.Path, test.a.Prefix)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s.CombinePathPrefix(%s) => got %v, want %v", spew.Sdump(test.b), spew.Sdump(test.a), got, test.want)
		}
	}
}
