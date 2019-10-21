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

package mesh

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	goversion "github.com/hashicorp/go-version"
	"gopkg.in/yaml.v2"

	"istio.io/operator/pkg/version"
	binversion "istio.io/operator/version"
)

func TestGetVersionCompatibleMap(t *testing.T) {
	type args struct {
		versionsURI string
		binVersion  *goversion.Version
		l           *logger
	}

	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-versions")
	testdataVersionsFilePath := filepath.Join(testDataDir, "input", "versions.yaml")
	operatorVersionsFilePath := "../../data/versions.yaml"
	nonexistentFilePath := "__nonexistent-versions.yaml"

	goVerNonexistent, _ := goversion.NewVersion("0.0.999")
	goVer133, _ := goversion.NewVersion("1.3.3")

	l := newLogger(true, os.Stdout, os.Stderr)

	b, err := ioutil.ReadFile(operatorVersionsFilePath)
	if err != nil {
		t.Fatal(err)
	}
	var vs []version.CompatibilityMapping
	if err := yaml.Unmarshal(b, &vs); err != nil {
		t.Fatal(err)
	}
	var curCm, ver133Cm *version.CompatibilityMapping
	for i := range vs {
		if binversion.OperatorBinaryGoVersion.Equal(vs[i].OperatorVersion) {
			curCm = &vs[i]
		}
		if goVer133.Equal(vs[i].OperatorVersion) {
			ver133Cm = &vs[i]
		}
	}

	if curCm == nil {
		t.Fatalf("OperatorBinaryGoVersion %v cannot be found in %s, "+
			"if OperatorBinaryGoVersion is updated to a new version, please also add it "+
			"into versions.yaml and generate the built-in vfs data.",
			binversion.OperatorBinaryGoVersion, operatorVersionsFilePath)
	}

	tests := []struct {
		name    string
		args    args
		want    *version.CompatibilityMapping
		wantErr error
	}{
		{
			name: "read the current binary version from data versions",
			args: args{
				versionsURI: operatorVersionsFilePath,
				binVersion:  binversion.OperatorBinaryGoVersion,
				l:           l,
			},
			want:    curCm,
			wantErr: nil,
		},
		{
			name: "read the current binary version from built-in version map",
			args: args{
				versionsURI: nonexistentFilePath,
				binVersion:  binversion.OperatorBinaryGoVersion,
				l:           l,
			},
			want:    curCm,
			wantErr: nil,
		},
		{
			name: "read version 133 from testdata",
			args: args{
				versionsURI: testdataVersionsFilePath,
				binVersion:  goVer133,
				l:           l,
			},
			want:    ver133Cm,
			wantErr: nil,
		},
		{
			name: "read nonexistent version from testdata",
			args: args{
				versionsURI: testdataVersionsFilePath,
				binVersion:  goVerNonexistent,
				l:           l,
			},
			want: nil,
			wantErr: fmt.Errorf("this operator version %s was not found in the version map",
				goVerNonexistent.String()),
		},
		{
			name: "read nonexistent version in built-in version map",
			args: args{
				versionsURI: nonexistentFilePath,
				binVersion:  goVerNonexistent,
				l:           l,
			},
			want: nil,
			wantErr: fmt.Errorf("this operator version %s was not found in the version map",
				goVerNonexistent.String()),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := getVersionCompatibleMap(tt.args.versionsURI, tt.args.binVersion, tt.args.l)
			if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", tt.want) {
				t.Errorf("got: %v, want: %v", got, tt.want)
			}
			if errToString(gotErr) != errToString(tt.wantErr) {
				t.Errorf("gotErr: %v, wantErr: %v", gotErr, tt.wantErr)
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
