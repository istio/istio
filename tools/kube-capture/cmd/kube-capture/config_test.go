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

package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/kr/pretty"
)

func TestUnmarshalKubeCaptureConfig(t *testing.T) {
	config := `
kubeConfigPath: a/b/c
context: d
istioNamespaces:
  - e1
  - e2
dryRun: true
strict: true
commandTimeout: 5m
maxArchiveSizeMb: 123
included:
  - ns1,ns2,-ns3/d1,d2,-d3/p1,p2,-p3/l1=lv1,l2=lv2,-l3=lv3/a1=av1,a2=av2,-a3=av3/c1,c2,-c3
  - ns4,ns5,-ns6/d4,d5,-d6/p4,p5,-p6/l4=lv4,l5=lv5,-l6=lv6/a4=av4,a5=av5,-a6=av6/c4,c5,-c6
excluded: 
  - ns7,ns8,-ns9/d7,d8,-d9/p7,p8,-p9/l7=lv7,l8=lv8,-l9=lv9/a7=av7,a8=av8,-a9=av9/c7,c8,-c9
startTime: 2002-10-02T10:00:00-05:00
endTime: 2002-10-02T10:00:00-05:00
since: 1m
criticalErrors:
  - e1
  - e2
whitelistedErrors:
  - e3
  - e4
gcsURL: f
uploadToGCS: true
`

	wantTime, err := time.Parse(time.RFC3339, "2002-10-02T10:00:00-05:00")
	if err != nil {
		t.Fatal(err)
	}
	want := &KubeCaptureConfig{
		KubeConfigPath:   "a/b/c",
		Context:          "d",
		IstioNamespaces:  []string{"e1", "e2"},
		DryRun:           true,
		Strict:           true,
		CommandTimeout:   Duration(5 * time.Minute),
		MaxArchiveSizeMb: 123,
		Included: []*SelectionSpec{
			{
				Namespaces: map[IncludeType][]string{
					Include: {"ns1", "ns2"},
					Exclude: {"ns3"},
				},
				Deployments: map[IncludeType][]string{
					Include: {"d1", "d2"},
					Exclude: {"d3"},
				},
				Pods: map[IncludeType][]string{
					Include: {"p1", "p2"},
					Exclude: {"p3"},
				},
				Containers: map[IncludeType][]string{
					Include: {"c1", "c2"},
					Exclude: {"c3"},
				},
				Labels: map[IncludeType]map[string]string{
					Include: {
						"l1": "lv1",
						"l2": "lv2",
					},
					Exclude: {
						"l3": "lv3",
					},
				},
				Annotations: map[IncludeType]map[string]string{
					Include: {
						"a1": "av1",
						"a2": "av2",
					},
					Exclude: {
						"a3": "av3",
					},
				},
			},
			{
				Namespaces: map[IncludeType][]string{
					Include: {"ns4", "ns5"},
					Exclude: {"ns6"},
				},
				Deployments: map[IncludeType][]string{
					Include: {"d4", "d5"},
					Exclude: {"d6"},
				},
				Pods: map[IncludeType][]string{
					Include: {"p4", "p5"},
					Exclude: {"p6"},
				},
				Containers: map[IncludeType][]string{
					Include: {"c4", "c5"},
					Exclude: {"c6"},
				},
				Labels: map[IncludeType]map[string]string{
					Include: {
						"l4": "lv4",
						"l5": "lv5",
					},
					Exclude: {
						"l6": "lv6",
					},
				},
				Annotations: map[IncludeType]map[string]string{
					Include: {
						"a4": "av4",
						"a5": "av5",
					},
					Exclude: {
						"a6": "av6",
					},
				},
			},
		},
		Excluded: []*SelectionSpec{
			{
				Namespaces: map[IncludeType][]string{
					Include: {"ns7", "ns8"},
					Exclude: {"ns9"},
				},
				Deployments: map[IncludeType][]string{
					Include: {"d7", "d8"},
					Exclude: {"d9"},
				},
				Pods: map[IncludeType][]string{
					Include: {"p7", "p8"},
					Exclude: {"p9"},
				},
				Containers: map[IncludeType][]string{
					Include: {"c7", "c8"},
					Exclude: {"c9"},
				},
				Labels: map[IncludeType]map[string]string{
					Include: {
						"l7": "lv7",
						"l8": "lv8",
					},
					Exclude: {
						"l9": "lv9",
					},
				},
				Annotations: map[IncludeType]map[string]string{
					Include: {
						"a7": "av7",
						"a8": "av8",
					},
					Exclude: {
						"a9": "av9",
					},
				},
			},
		},
		StartTime:         wantTime,
		EndTime:           wantTime,
		Since:             Duration(time.Minute),
		CriticalErrors:    []string{"e1", "e2"},
		WhitelistedErrors: []string{"e3", "e4"},
		GCSURL:            "f",
		UploadToGCS:       true,
	}

	got := &KubeCaptureConfig{}
	if err := yaml.Unmarshal([]byte(config), got); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n%s\nwant:\n%s\n", pretty.Sprint(got), pretty.Sprint(want))
	}
}
