// Copyright 2016 Istio Authors
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

package template

import (
	"reflect"
	"strings"
	"testing"

	"istio.io/mixer/pkg/adapter"
)

func TestGetTemplateInfo(t *testing.T) {
	for _, tst := range []struct {
		tmplToFind   string
		allTmplInfos map[string]Info
		expected     Info
		present      bool
	}{
		{"ValidTmpl", map[string]Info{"ValidTmpl": {BldrName: "FooTmplBuilder"}},
			Info{BldrName: "FooTmplBuilder"}, true},
		{"unknown template", nil, Info{}, false},
	} {
		t.Run(tst.tmplToFind, func(t *testing.T) {
			tdf := NewRepository(tst.allTmplInfos)
			k, rpresent := tdf.GetTemplateInfo(tst.tmplToFind)
			if rpresent != tst.present ||
				!reflect.DeepEqual(k.CtrCfg, tst.expected.CtrCfg) ||
				!reflect.DeepEqual(reflect.TypeOf(k.InferType), reflect.TypeOf(tst.expected.InferType)) {
				t.Errorf("got GetInstanceDefaultConfig(%s) = {%v,%v,%v}, want {%v,%v,%v}", tst.tmplToFind,
					k.CtrCfg, reflect.TypeOf(k.InferType), rpresent,
					tst.expected.CtrCfg, reflect.TypeOf(tst.expected.InferType), tst.present)
			}
		})
	}
}

func TestSupportsTemplate(t *testing.T) {
	for _, tst := range []struct {
		tmplToCheck  string
		allTmplInfos map[string]Info
		expectedErr  string
	}{
		{"ValidTmpl", map[string]Info{"ValidTmpl": {BldrName: "ValidTmplBuilder",
			SupportsTemplate: func(h adapter.HandlerBuilder) bool { return true }}}, ""},
		{"unknown template", nil, "is not one of the allowed supported templates"},
		{"interface_not_implemented", map[string]Info{"interface_not_implemented": {BldrName: "interface_not_implementedBuilder",
			SupportsTemplate: func(h adapter.HandlerBuilder) bool { return false }}},
			"does not implement interface interface_not_implementedBuilder"},
	} {
		t.Run(tst.tmplToCheck, func(t *testing.T) {
			tdf := NewRepository(tst.allTmplInfos)
			actualSuccess, cerr := tdf.SupportsTemplate(nil, tst.tmplToCheck)
			expectSuccess := tst.expectedErr == ""
			if expectSuccess != actualSuccess {
				t.Fatalf("got SupportsTemplate(%s) = %t, expect %t", tst.tmplToCheck, actualSuccess,
					expectSuccess)
			}
			if !strings.Contains(cerr, tst.expectedErr) {
				t.Errorf("got error for SupportsTemplate(%s) = '%s', want string containing '%s'",
					tst.tmplToCheck, cerr, tst.expectedErr)
			}
		})
	}
}
