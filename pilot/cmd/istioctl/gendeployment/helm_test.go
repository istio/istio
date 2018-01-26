// Copyright 2017 Istio Authors.
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

package gendeployment

import (
	"io/ioutil"
	"path"
	"testing"
)

func TestValuesFromInstallation(t *testing.T) {
	tests := []struct {
		name   string
		in     *installation
		golden string
	}{
		{"default", defaultInstall(), "default-values"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contents := valuesFromInstallation(tt.in)
			want := readGolden(t, tt.golden)
			if contents != want {
				t.Fatalf("valuesFromInstallation(%v)\n got %q\nwant %q", tt.in, contents, want)
			}
		})
	}
}

func readGolden(t *testing.T, name string) string {
	t.Helper()

	p := path.Join("testdata", name+".yaml.golden")
	data, err := ioutil.ReadFile(p)
	if err != nil {
		t.Fatalf("failed to read %q", p)
	}
	return string(data)
}
