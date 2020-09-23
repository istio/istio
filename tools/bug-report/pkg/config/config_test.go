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

package config

import (
	"reflect"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/google/go-cmp/cmp"
	"github.com/kr/pretty"
)

func TestUnmarshalKubeCaptureConfig(t *testing.T) {
	config := `
kubeConfigPath: a/b/c
context: d
istioNamespace: e1
dryRun: true
fullSecrets: true
commandTimeout: 5m
include:
  - ns1,ns2/d1,d2/p1,p2/l1=lv1,l2=lv2/a1=av1,a2=av2/c1,c2
  - ns4,ns5/d4,d5/p4,p5/l4=lv4,l5=lv5/a4=av4,a5=av5/c4,c5
exclude: 
  - ns7,ns8/d7,d8/p7,p8/l7=lv7,l8=lv8/a7=av7,a8=av8/c7,c8
startTime: 2002-10-02T10:00:00-05:00
endTime: 2002-10-02T10:00:00-05:00
since: 1m
criticalErrors:
  - e1
  - e2
ignoredErrors:
  - e3
  - e4
`
	wantTime, err := time.Parse(time.RFC3339, "2002-10-02T10:00:00-05:00")
	if err != nil {
		t.Fatal(err)
	}
	want := &BugReportConfig{
		KubeConfigPath: "a/b/c",
		Context:        "d",
		IstioNamespace: "e1",
		DryRun:         true,
		FullSecrets:    true,
		CommandTimeout: Duration(5 * time.Minute),
		Include: []*SelectionSpec{
			{
				Namespaces:  []string{"ns1", "ns2"},
				Deployments: []string{"d1", "d2"},
				Pods:        []string{"p1", "p2"},
				Containers:  []string{"c1", "c2"},
				Labels: map[string]string{
					"l1": "lv1",
					"l2": "lv2",
				},
				Annotations: map[string]string{
					"a1": "av1",
					"a2": "av2",
				},
			},
			{
				Namespaces:  []string{"ns4", "ns5"},
				Deployments: []string{"d4", "d5"},
				Pods:        []string{"p4", "p5"},
				Containers:  []string{"c4", "c5"},
				Labels: map[string]string{
					"l4": "lv4",
					"l5": "lv5",
				},
				Annotations: map[string]string{
					"a4": "av4",
					"a5": "av5",
				},
			},
		},
		Exclude: []*SelectionSpec{
			{
				Namespaces:  []string{"ns7", "ns8"},
				Deployments: []string{"d7", "d8"},
				Pods:        []string{"p7", "p8"},
				Containers:  []string{"c7", "c8"},
				Labels: map[string]string{
					"l7": "lv7",
					"l8": "lv8",
				},
				Annotations: map[string]string{
					"a7": "av7",
					"a8": "av8",
				},
			},
		},
		StartTime:      wantTime,
		EndTime:        wantTime,
		Since:          Duration(time.Minute),
		CriticalErrors: []string{"e1", "e2"},
		IgnoredErrors:  []string{"e3", "e4"},
	}

	got := &BugReportConfig{}
	if err := yaml.Unmarshal([]byte(config), got); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n%s\nwant:\n%s\n\ndiff (-got, +want):\n%s\n", pretty.Sprint(got), pretty.Sprint(want), cmp.Diff(got, want))
	}
}
