// Copyright 2019 Istio Authors
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

package version

import (
	"reflect"
	"testing"

	"istio.io/operator/pkg/util"

	"github.com/kr/pretty"
	"gopkg.in/yaml.v2"
)

func TestVersion(t *testing.T) {
	tests := []struct {
		desc    string
		yamlStr string
		want    Version
		wantErr string
	}{
		{
			desc: "nil success",
		},
		{
			desc:    "major success",
			yamlStr: "1",
			want:    NewVersion(1, 0, 0, ""),
		},
		{
			desc:    "major fail",
			yamlStr: "1..",
			wantErr: `Malformed version: 1..`,
		},
		{
			desc:    "major fail prefix",
			yamlStr: ".1",
			wantErr: `Malformed version: .1`,
		},
		{
			desc:    "minor success",
			yamlStr: "1.2",
			want:    NewVersion(1, 2, 0, ""),
		},
		{
			desc:    "minor fail",
			yamlStr: "1.1..",
			wantErr: `Malformed version: 1.1..`,
		},
		{
			desc:    "patch success",
			yamlStr: "1.2.3",
			want:    NewVersion(1, 2, 3, ""),
		},
		{
			desc:    "patch fail",
			yamlStr: "1.1.-1",
			wantErr: `Malformed version: 1.1.-1`,
		},
		{
			desc:    "suffix success",
			yamlStr: "1.2.3-istio-test",
			want:    NewVersion(1, 2, 3, "istio-test"),
		},
		{
			desc:    "suffix fail",
			yamlStr: ".1.1.1-something",
			wantErr: `Malformed version: .1.1.1-something`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := Version{}
			err := yaml.Unmarshal([]byte(tt.yamlStr), &got)
			if gotErr, wantErr := errToString(err), tt.wantErr; gotErr != wantErr {
				t.Fatalf("yaml.Unmarshal(%s): got error: %s, want error: %s", tt.desc, gotErr, wantErr)
			}
			if tt.wantErr == "" && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s: got:\n%s\nwant:\n%s\n", tt.desc, pretty.Sprint(got), pretty.Sprint(tt.want))
			}
		})
	}
}

func TestVersions(t *testing.T) {
	tests := []struct {
		desc    string
		yamlStr string
		wantErr string
	}{
		{
			desc: "empty",
		},
		{
			desc: "simple",
			yamlStr: `
operatorVersion: 1.3.0
operatorVersionRange: 1.3.0
recommendedIstioVersions: 1.3.0
supportedIstioVersions: 1.3.0
`,
		},
		{
			desc: "complex",
			yamlStr: `
operatorVersion: 1.3.0
operatorVersionRange: 1.3.0
recommendedIstioVersions: '>= 1, < 1.4'
supportedIstioVersions: '> 1.1, < 1.4.0, = 1.5.2'
`,
		},
		{
			desc: "partial",
			yamlStr: `
operatorVersion: 1.3.0
operatorVersionRange: 1.3.0
supportedIstioVersions: "> 1.1, < 1.4.0"
`,
		},
		{
			desc: "missing operatorVersion",
			yamlStr: `
supportedIstioVersions: 1.3.0
recommendedIstioVersions: 1.3.0
`,
			wantErr: `operatorVersion must be set`,
		},
		{
			desc: "missing supportedIstioVersions",
			yamlStr: `
operatorVersion: 1.3.0
recommendedIstioVersions: 1.3.0
`,
			wantErr: `supportedIstioVersions must be set`,
		},
		{
			desc: "unknown field",
			yamlStr: `
operatorVersion: 1.3.0
supportedIstioVersions: "> 1.1, < 1.4.0, = 1.5.2"
badField: ">= 1, < 1.4"
`,
			wantErr: `yaml: unmarshal errors:
  line 4: field badField not found in type version.inStruct`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := &CompatibilityMapping{}
			err := yaml.UnmarshalStrict([]byte(tt.yamlStr), got)
			if gotErr, wantErr := errToString(err), tt.wantErr; gotErr != wantErr {
				t.Fatalf("yaml.Unmarshal(%s): got error: %s, want error: %s", tt.desc, gotErr, wantErr)
			}
			if tt.wantErr != "" {
				return
			}
			y, err := yaml.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			ys := string(y)
			if yd := util.YAMLDiff(tt.yamlStr, ys); yd != "" {
				t.Errorf("%s: got:\n%s\nwant:\n%s\ndiff:\n%s\n", tt.desc, ys, tt.yamlStr, yd)
			}
		})
	}

}

// errToString returns the string representation of err and the empty string if
// err is nil.
func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
