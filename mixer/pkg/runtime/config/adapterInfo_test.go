// Copyright 2018 Istio Authors.
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

// nolint
//go:generate protoc testdata/foo.proto -otestdata/foo.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
//go:generate protoc testdata/adptCfg.proto -otestdata/adptCfg.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
//go:generate protoc testdata/bar.proto -otestdata/bar.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
//go:generate protoc testdata/baz.proto -otestdata/baz.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
//go:generate protoc testdata/unsupportedPkgName.proto -otestdata/unsupportedPkgName.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
//go:generate protoc testdata/reqOptionNotFound.proto -otestdata/reqOptionNotFound.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
//go:generate protoc testdata/reqOptionTmplNameNotFound.proto -otestdata/reqOptionTmplNameNotFound.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
//go:generate protoc testdata/foo.proto testdata/bar.proto -otestdata/badfoobar.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.

package config

import (
	"bytes"
	"encoding/base64"
	"io/ioutil"
	"reflect"
	"strings"
	"testing"

	multierror "github.com/hashicorp/go-multierror"

	adapter "istio.io/api/mixer/adapter/model/v1beta1"
)

type testdata struct {
	name      string
	infos     []adapter.Info
	wantErrs  []string
	wantInfos map[string][]string
	wantTmpls []string
}

var fooTmpl = getFileDescSetBase64("testdata/foo.descriptor")
var adptCfg = getFileDescSetBase64("testdata/adptCfg.descriptor")
var barTmpl = getFileDescSetBase64("testdata/bar.descriptor")
var bazTmpl = getFileDescSetBase64("testdata/baz.descriptor")
var badfoobarTmpl = getFileDescSetBase64("testdata/badfoobar.descriptor")
var unsupportedPkgNameTmpl = getFileDescSetBase64("testdata/unsupportedPkgName.descriptor")
var reqOptionTmplNameNotFoundTmpl = getFileDescSetBase64("testdata/reqOptionTmplNameNotFound.descriptor")
var reqOptionNotFoundTmpl = getFileDescSetBase64("testdata/reqOptionNotFound.descriptor")
var notFileDescriptorSet = getFileDescSetBase64("testdata/foo.proto")

func TestNew(t *testing.T) {

	for _, td := range []testdata{
		{
			name: "adapters with varying templates",
			infos: []adapter.Info{
				{
					Name:      "a1",
					Templates: []string{fooTmpl, barTmpl},
				},
				{
					Name:      "a2",
					Templates: []string{bazTmpl},
				},
				{
					Name:      "a3",
					Templates: []string{},
				},
			},
			wantInfos: map[string][]string{"a1": {"foo", "bar"}, "a2": {"baz"}, "a3": {}},
			wantTmpls: []string{"foo", "bar", "baz"},
		},
		{
			name: "adapters with duplicate templates",
			infos: []adapter.Info{
				{
					Name:      "a1",
					Templates: []string{fooTmpl, barTmpl},
				},
				{
					Name:      "a2",
					Templates: []string{fooTmpl},
				},
			},
			wantInfos: map[string][]string{"a1": {"foo", "bar"}, "a2": {"foo"}},
			wantTmpls: []string{"foo", "bar"},
		},
		{
			name: "only templates no adapters",
			infos: []adapter.Info{
				{
					Name:      "",
					Templates: []string{fooTmpl, barTmpl},
				},
				{
					Name:      "",
					Templates: []string{bazTmpl},
				},
			},
			wantTmpls: []string{"foo", "bar", "baz"},
		},
		{
			name: "adapters with bad templates not registered",
			infos: []adapter.Info{
				{
					Name:      "bad_adapter",
					Templates: []string{fooTmpl, badfoobarTmpl},
				},
				{
					Name:      "good_adapter",
					Templates: []string{barTmpl},
				},
			},
			wantInfos: map[string][]string{"good_adapter": {"bar"}},
			wantTmpls: []string{"bar"},
			wantErrs:  []string{"Only one proto file is allowed with this options"},
		},
		{
			name: "error duplicate adapters",
			infos: []adapter.Info{
				{
					Name:      "a1",
					Templates: []string{},
				},
				{
					Name:      "a1",
					Templates: []string{},
				},
				{
					Name:      "a3",
					Templates: []string{},
				},
			},
			wantInfos: map[string][]string{"a1": {}, "a3": {}},
			wantErrs:  []string{"duplicate registration for adapter 'a1'"},
		},
		{
			name: "error bad template",
			infos: []adapter.Info{
				{
					Name:      "a1",
					Templates: []string{fooTmpl, barTmpl},
				},
				{
					Name:      "bad_adapter_since_bad_template",
					Templates: []string{badfoobarTmpl},
				},
			},
			wantInfos: map[string][]string{"a1": {"foo", "bar"}},
			wantErrs:  []string{"Only one proto file is allowed with this options"},
			wantTmpls: []string{"foo", "bar"},
		},
		{
			name: "error bad base64 string",
			infos: []adapter.Info{
				{
					Name:      "a1",
					Templates: []string{"error base 64 string"},
				},
			},
			wantErrs: []string{"illegal base64 data at input byte"},
		},
		{
			name: "error bad fds string",
			infos: []adapter.Info{
				{
					Name:      "a1",
					Templates: []string{notFileDescriptorSet},
				},
			},
			wantErrs: []string{"unknown wire type 7"},
		},
		{
			name: "error unsupported template name",
			infos: []adapter.Info{
				{
					Name:      "a1",
					Templates: []string{unsupportedPkgNameTmpl},
				},
			},
			wantErrs: []string{"the template name 'foo123' must match the regex '^[a-zA-Z]+$'"},
		},
		{
			name: "error template variety not found",
			infos: []adapter.Info{
				{
					Name:      "a1",
					Templates: []string{reqOptionNotFoundTmpl},
				},
			},
			wantErrs: []string{"there has to be one proto file that has the extension"},
		},
		{
			name: "error template name not found",
			infos: []adapter.Info{
				{
					Name:      "a1",
					Templates: []string{reqOptionTmplNameNotFoundTmpl},
				},
			},
			wantErrs: []string{"proto files testdata/reqOptionTmplNameNotFound.proto is missing required template_name option"},
		},
		{
			name: "different adapter with mix of good bad templates",
			infos: []adapter.Info{
				{
					Name:      "bad_adapter",
					Templates: []string{fooTmpl, badfoobarTmpl},
				},
				{
					Name:      "good_adapter",
					Templates: []string{fooTmpl, barTmpl},
				},
			},
			wantInfos: map[string][]string{"good_adapter": {"foo", "bar"}},
			wantTmpls: []string{"foo", "bar"},
			wantErrs:  []string{"Only one proto file is allowed with this options"},
		},
		{
			name: "valid adapter config",
			infos: []adapter.Info{
				{
					Name:      "a1",
					Templates: []string{fooTmpl},
					Config:    adptCfg,
				},
			},
			wantInfos: map[string][]string{"a1": {"foo"}},
			wantTmpls: []string{"foo"},
		},
		{
			name: "error adapter config without Param message",
			infos: []adapter.Info{
				{
					Name:      "a1",
					Templates: []string{fooTmpl},
					Config:    fooTmpl, // does not contain "Param" msg
				},
			},
			wantErrs: []string{"cannot find message named 'Param' in the adapter configuration descriptor"},
		},
		{
			name: "error bad adapter config",
			infos: []adapter.Info{
				{
					Name:      "a1",
					Templates: []string{fooTmpl},
					Config:    notFileDescriptorSet, // bogus
				},
			},
			wantErrs: []string{"can't skip unknown wire type 7 for descriptor.FileDescriptorSet"},
		},
	} {
		t.Run(td.name, func(t *testing.T) {
			infos := make([]*adapter.Info, len(td.infos))
			for i, info := range td.infos {
				infoCpy := info
				infos[i] = &infoCpy
			}
			reg, err := NewAdapterInfoRegistry(infos)
			if len(td.wantErrs) == 0 {
				if err != nil {
					t.Fatalf("want no error got '%v;", err)
				}
			} else {
				if err == nil {
					t.Fatalf("want errors '%v'; got no errors", td.wantErrs)
				}

				gotErrs := err.(*multierror.Error).Errors
				wantErrs := td.wantErrs
				if len(gotErrs) != len(wantErrs) {
					t.Fatalf("want %d errors as '%v'; got %d as '%v'", len(wantErrs), wantErrs, len(gotErrs), gotErrs)
				}

				for i, wantErr := range td.wantErrs {
					gotErr := err.(*multierror.Error).Errors[i]
					if !strings.Contains(gotErr.Error(), wantErr) {
						t.Errorf("want error '%s' at location %d; got '%s'", wantErr, i, gotErr.Error())
					}
				}
			}

			// Ensure infos match
			if len(reg.adapters) != len(td.wantInfos) {
				t.Fatalf("want %d infos with names '%v'; got %d as '%v'",
					len(td.wantInfos), td.wantInfos, len(reg.adapters), reg.adapters)
			}
			if len(reg.GetAdapters()) != len(td.wantInfos) {
				t.Fatalf("want %d infos with names '%v'; got %d as '%v'",
					len(td.wantInfos), td.wantInfos, len(reg.GetAdapters()), reg.GetAdapters())
			}
			for k, wantInfo := range td.wantInfos {
				if adptMeta, ok := reg.adapters[k]; !ok {
					t.Errorf("want info '%s' to be present; got '%v'", k, reg.adapters)
				} else if !reflect.DeepEqual(wantInfo, adptMeta.SupportedTemplates) {
					t.Errorf("want supported templates for info '%s' to be '%v'; got '%v'", k, wantInfo, adptMeta.SupportedTemplates)
				}
			}

			// Ensure templates match
			if len(reg.templates) != len(td.wantTmpls) {
				t.Fatalf("want %d templates with names '%v'; got %d as '%v'",
					len(td.wantTmpls), td.wantTmpls, len(reg.templates), reg.templates)
			}
			if len(reg.GetTemplates()) != len(td.wantTmpls) {
				t.Fatalf("want %d templates with names '%v'; got %d as '%v'",
					len(td.wantTmpls), td.wantTmpls, len(reg.GetTemplates()), reg.GetTemplates())
			}
			for _, wantTmpl := range td.wantTmpls {
				if _, ok := reg.templates[wantTmpl]; !ok {
					t.Errorf("want template '%s' to be present; got '%v'", wantTmpl, reg.templates)
				}
			}
		})
	}
}

func getFileDescSetBase64(path string) string {
	byts, _ := ioutil.ReadFile(path)
	var b bytes.Buffer
	encoder := base64.NewEncoder(base64.StdEncoding, &b)
	_, _ = encoder.Write(byts)
	_ = encoder.Close()
	return b.String()
}
