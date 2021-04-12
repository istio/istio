// Copyright Istio Authors
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

package revision

import (
	"io/ioutil"
	"reflect"
	"testing"
)

const testRevisionConfigYAML = `revisions:
- name: "asm-revision-istiodca"
  ca: "CITADEL"
  overlay: "overlay/trustanchor-meshca.yaml"
- name: "asm-revision-meshca"
  ca: "MESHCA"
  overlay: "overlay/trustdomain-migrate.yaml"
`

const testRevisionConfigJSON = `{
  "revisions": [
    {
      "name": "asm-revision-istiodca",
      "ca": "CITADEL",
      "overlay": "overlay/trustanchor-meshca.yaml"
    },
    {
      "name": "asm-revision-meshca",
      "ca": "MESHCA",
      "overlay": "overlay/trustdomain-migrate.yaml"
    }
  ]
}
`

func TestParseRevisionConfig(t *testing.T) {
	tcs := []struct {
		name           string
		configContents string
	}{
		{
			"yaml revision config",
			testRevisionConfigYAML,
		},
		{
			"json revision config",
			testRevisionConfigJSON,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			f, err := ioutil.TempFile("", "revision-config")
			if err != nil {
				t.Fatalf("failed creating temp revision config file: %v", err)
			}
			_, err = f.WriteString(tc.configContents)
			if err != nil {
				t.Fatalf("failed writing to temp revision config file: %v", err)
			}
			revisionConfigs, err := ParseRevisionConfig(f.Name())
			if err != nil {
				t.Fatalf("failed to parse revision config for %q: %v", f.Name(), err)
			}
			want := RevisionConfigs{Configs: []RevisionConfig{
				{
					Name:    "asm-revision-istiodca",
					CA:      "CITADEL",
					Overlay: "overlay/trustanchor-meshca.yaml",
				},
				{
					Name:    "asm-revision-meshca",
					CA:      "MESHCA",
					Overlay: "overlay/trustdomain-migrate.yaml",
				},
			}}
			if !reflect.DeepEqual(revisionConfigs, &want) {
				t.Errorf("expected revision configs %v, got %v", want, revisionConfigs)
			}
		})
	}
}
