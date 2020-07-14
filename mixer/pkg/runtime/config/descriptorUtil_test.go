// Copyright Istio Authors.
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
//go:generate $REPO_ROOT/bin/protoc.sh testdata/foo.proto -otestdata/foo.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh testdata/adptCfg.proto -otestdata/adptCfg.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh testdata/unsupportedPkgName.proto -otestdata/unsupportedPkgName.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh testdata/reqOptionNotFound.proto -otestdata/reqOptionNotFound.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh testdata/reqOptionTmplNameNotFound.proto -otestdata/reqOptionTmplNameNotFound.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh testdata/foo.proto testdata/bar.proto -otestdata/badfoobar.descriptor -I.

package config

import (
	"bytes"
	"encoding/base64"
	"io/ioutil"
	"strings"
	"testing"
)

type testdata struct {
	name          string
	wantErr       string
	DescriptorStr string
	wantName      string
}

var fooTmpl = getFileDescSetBase64("testdata/foo.descriptor")
var adptCfg = getFileDescSetBase64("testdata/adptCfg.descriptor")
var badfoobarTmpl = getFileDescSetBase64("testdata/badfoobar.descriptor")
var unsupportedPkgNameTmpl = getFileDescSetBase64("testdata/unsupportedPkgName.descriptor")
var reqOptionTmplNameNotFoundTmpl = getFileDescSetBase64("testdata/reqOptionTmplNameNotFound.descriptor")
var reqOptionNotFoundTmpl = getFileDescSetBase64("testdata/reqOptionNotFound.descriptor")
var notFileDescriptorSet = "X"

func TestTemplates(t *testing.T) {

	for _, td := range []testdata{
		{
			name:          "valid template",
			DescriptorStr: fooTmpl,
			wantName:      "foo",
		},
		{
			name:          "adapters with bad templates not registered",
			DescriptorStr: badfoobarTmpl,
			wantErr:       "both have the option ",
		},
		{
			name:          "error bad base64 string",
			DescriptorStr: "error base 64 string",
			wantErr:       "illegal base64 data ",
		},
		{
			name:          "error bad fds string",
			DescriptorStr: notFileDescriptorSet,
			wantErr:       "unexpected EOF",
		},
		{
			name:          "error unsupported template name",
			DescriptorStr: unsupportedPkgNameTmpl,
			wantErr:       "the template name 'foo123' must match the regex '^[a-zA-Z]+$'",
		},
		{
			name:          "error template variety not found",
			DescriptorStr: reqOptionNotFoundTmpl,
			wantErr:       "there has to be one proto file that has the extension",
		},
		{
			name:          "error template name not found",
			DescriptorStr: reqOptionTmplNameNotFoundTmpl,
			wantErr:       "proto files testdata/reqOptionTmplNameNotFound.proto is missing required template_name option",
		},
	} {
		t.Run(td.name, func(t *testing.T) {
			_, _, name, _, err := GetTmplDescriptor(td.DescriptorStr)
			if td.wantErr == "" {
				if err != nil {
					t.Fatalf("want no error got '%v;", err)
				}
				if name != td.wantName {
					t.Errorf("want template '%s'; got '%v'", td.wantName, name)
				}
			} else {
				if err == nil {
					t.Fatalf("want error '%s'; got no error", td.wantErr)
				}
				if !strings.Contains(err.Error(), td.wantErr) {
					t.Errorf("want error '%s'; got error '%s'", td.wantErr, err.Error())
				}
			}
		})
	}
}

func TestAdapters(t *testing.T) {
	for _, td := range []testdata{
		{
			name:          "valid adapter config",
			DescriptorStr: adptCfg,
		},
		{
			name:          "error adapter config without Param message",
			DescriptorStr: fooTmpl, // does not contain "Param" msg
			wantErr:       "cannot find message named 'Params' in the adapter configuration descriptor",
		},
		{
			name:          "error bad adapter config",
			DescriptorStr: notFileDescriptorSet, // bogus
			wantErr:       "unexpected EOF",
		},
		{
			name:          "empty descriptor",
			DescriptorStr: "", // empty desc allowed
		},
	} {
		t.Run(td.name, func(t *testing.T) {
			_, _, err := GetAdapterCfgDescriptor(td.DescriptorStr)
			if td.wantErr == "" {
				if err != nil {
					t.Fatalf("want no error got '%v;", err)
				}
			} else {
				if err == nil {
					t.Fatalf("want error '%s'; got no error", td.wantErr)
				}
				if !strings.Contains(err.Error(), td.wantErr) {
					t.Errorf("want error '%s'; got error '%s'", td.wantErr, err.Error())
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
