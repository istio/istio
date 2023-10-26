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

package filter

import (
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	cluster2 "istio.io/istio/tools/bug-report/pkg/cluster"
	config2 "istio.io/istio/tools/bug-report/pkg/config"
)

var (
	testClusterResourcesTree = `
ns1:
  d1:
    p1:
      c1: null
      c2: null
    p2:
      c3: null
  d2:
    p3:
      c4: null
      c5: null
    p4:
      c6: null
`
	testClusterResourcesLabels = `
ns1/p1:
  l1: v1
  l2: v2
ns1/p2:
  l1: v1
  l2: v22
  l3: v3
ns1/p3:
  l1: v1
  l2: v2
  l3: v3
  l4: v4
`
	testClusterResourcesAnnotations = `
ns1/p1:
  k2: v2
ns1/p2:
  k1: v1
  k2: v2
  k3: v33
ns1/p3:
  k1: v1
  k2: v22
  k3: v3
ns1/p4:
  k1: v1
  k4: v4
`

	p1c1 = "ns1/d1/p1/c1"
	p1c2 = "ns1/d1/p1/c2"
	p2c3 = "ns1/d1/p2/c3"
	p3c4 = "ns1/d2/p3/c4"
	p3c5 = "ns1/d2/p3/c5"
	p4c6 = "ns1/d2/p4/c6"

	p1 = []string{p1c1, p1c2}
	p2 = []string{p2c3}
	p3 = []string{p3c4, p3c5}
	p4 = []string{p4c6}

	d1 = append(p1, p2...)
	d2 = append(p3, p4...)

	ns1 = append(d1, d2...)

	testClusterResources = &cluster2.Resources{
		Root:        make(map[string]any),
		Labels:      make(map[string]map[string]string),
		Annotations: make(map[string]map[string]string),
	}
)

func init() {
	if err := yaml.Unmarshal([]byte(testClusterResourcesTree), &testClusterResources.Root); err != nil {
		panic(err)
	}
	if err := yaml.Unmarshal([]byte(testClusterResourcesLabels), &testClusterResources.Labels); err != nil {
		panic(err)
	}
	if err := yaml.Unmarshal([]byte(testClusterResourcesAnnotations), &testClusterResources.Annotations); err != nil {
		panic(err)
	}
}

func TestGetMatchingPaths(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name    string
		config  string
		want    []string
		wantErr string
	}{
		{
			name:   "wantEmpty",
			config: ``,
			want:   ns1,
		},
		{
			name: "glob deployment",
			config: `
include:
  - n*/*`,
			want: ns1,
		},
		{
			name: "glob pod",
			config: `
include:
  - n*/*/*`,
			want: ns1,
		},
		{
			name: "glob container",
			config: `
include:
  - n*/*/*/*/*/*`,
			want: ns1,
		},
		{
			name: "glob container slashes",
			config: `
include:
  - /////`,
			want: ns1,
		},
		{
			name: "glob part namespace",
			config: `
include:
  - n*`,
			want: ns1,
		},
		{
			name: "glob part multiple",
			config: `
include:
  - n*/d*/p*/*/*/c*`,
			want: ns1,
		},
		{
			name: "glob namespace no match",
			config: `
include:
  - bad*`,
			want: nil,
		},
		{
			name: "glob deployments",
			config: `
include:
  - ns1/d*`,
			want: ns1,
		},
		{
			name: "glob deployments no match",
			config: `
include:
  - ns1/bad*`,
			want: nil,
		},
		{
			name: "glob deployments some",
			config: `
include:
  - ns1/d1*`,
			want: d1,
		},
		{
			name: "glob container all",
			config: `
include:
  - /*/*/*/*/c*`,
			want: ns1,
		},
		{
			name: "labels",
			config: `
include:
  - ns1,ns2/d1,d2/p1,p2,p3,p4/l1=v1,l2=v2`,
			want: joinSlices(p1, p3),
		},
		{
			name: "glob deployments with labels",
			config: `
include:
  - ns1,ns2/d*/p1,p2,p3,p4/l1=v1,l2=v2`,
			want:    joinSlices(p1, p3),
			wantErr: "",
		},
		{
			name: "glob deployments with labels and annotations",
			config: `
include:
  - ns1,ns2/d1,d2/p1,p2,p3,p4/l1=v1,l2=v2/k2=v2`,
			want:    p1,
			wantErr: "",
		},
		{
			name: "label wildcards",
			config: `
include:
  - n*/*/*/l1=v1,l2=v2*`,
			want:    joinSlices(p1, p2, p3),
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &config2.BugReportConfig{}
			if err := yaml.Unmarshal([]byte(tt.config), config); err != nil {
				t.Fatal(err)
			}

			paths, err := GetMatchingPaths(config, testClusterResources)
			if err != nil {
				t.Fatal(err)
			}
			g.Expect(paths).Should(ConsistOf(tt.want))
		})
	}
}

func TestGetMatchingPathsMultiple(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name    string
		config  string
		want    []string
		wantErr string
	}{
		{
			name: "bad namespace",
			config: `
include:
  - bad*
  - ns1`,
			want: ns1,
		},
		{
			name: "bad deployments",
			config: `
include:
  - ns1/bad*
  - ns1/d1`,
			want: d1,
		},
		{
			name: "container multiple",
			config: `
include:
  - /*/*/*/*/c1
  - /*/*/*/*/c2`,
			want: p1,
		},
		{
			name: "labels",
			config: `
include:
  - ns1,ns2/d1,d2/p1,p2,p3,p4/l1=v1
  - ///l2=v2`,
			want: joinSlices(p1, p2, p3),
		},
		{
			name: "labels and annotations",
			//  - ///l1=v1,l2=v2
			config: `
include:
  - ////k4=v4`,
			want:    joinSlices(p4),
			wantErr: "",
		},
		{
			name: "label wildcards",
			config: `
include:
  - n*/*/*/l1=v1
  - n*/*/*/l2=v2*`,
			want:    joinSlices(p1, p2, p3),
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &config2.BugReportConfig{}
			if err := yaml.Unmarshal([]byte(tt.config), config); err != nil {
				t.Fatal(err)
			}

			paths, err := GetMatchingPaths(config, testClusterResources)
			if err != nil {
				t.Fatal(err)
			}
			g.Expect(paths).Should(ConsistOf(tt.want))
		})
	}
}

func TestGetMatchingPathsExclude(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name    string
		config  string
		want    []string
		wantErr string
	}{
		{
			name: "exclude all",
			config: `
exclude:
  - /////
`,
			want: nil,
		},
		{
			name: "glob deployment",
			config: `
exclude:
  - n*/*`,
			want: nil,
		},
		{
			name: "glob pod",
			config: `
exclude:
  - n*/*/*`,
			want: nil,
		},
		{
			name: "glob container",
			config: `
exclude:
  - n*/*/*/*/*/*`,
			want: nil,
		},
		{
			name: "glob container slashes",
			config: `
exclude:
  - /////`,
			want: nil,
		},
		{
			name: "glob part namespace",
			config: `
exclude:
  - n*`,
			want: nil,
		},
		{
			name: "glob part multiple",
			config: `
exclude:
  - n*/d*/p*/*/*/c*`,
			want: nil,
		},
		{
			name: "glob namespace no match",
			config: `
exclude:
  - bad*`,
			want: ns1,
		},
		{
			name: "glob deployments",
			config: `
exclude:
  - ns1/d*`,
			want: nil,
		},
		{
			name: "glob deployments no match",
			config: `
exclude:
  - ns1/bad*`,
			want: ns1,
		},
		{
			name: "glob deployments some",
			config: `
exclude:
  - ns1/d1*`,
			want: d2,
		},
		{
			name: "glob container all",
			config: `
exclude:
  - /*/*/*/*/c*`,
			want: nil,
		},
		{
			name: "labels",
			config: `
exclude:
  - ns1,ns2/d1,d2/p1,p2,p3,p4/l1=v1,l2=v2`,
			want: joinSlices(p2, p4),
		},
		{
			name: "glob deployments with labels",
			config: `
exclude:
  - ns1,ns2/*/p1,p2,p3,p4/l1=v1,l2=v2`,
			want:    joinSlices(p2, p4),
			wantErr: "",
		},
		{
			name: "glob deployments with labels and annotations",
			config: `
exclude:
  - ns1,ns2/d1,d2/p1,p2,p3,p4/l1=v1,l2=v2/k2=v2`,
			want:    joinSlices(p2, p3, p4),
			wantErr: "",
		},
		{
			name: "label wildcards",
			config: `
exclude:
  - ///l1=v1,l2=v2*`,
			want:    p4,
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &config2.BugReportConfig{}
			if err := yaml.Unmarshal([]byte(tt.config), config); err != nil {
				t.Fatal(err)
			}

			paths, err := GetMatchingPaths(config, testClusterResources)
			if err != nil {
				t.Fatal(err)
			}
			g.Expect(paths).Should(ConsistOf(tt.want))
		})
	}
}

func joinSlices(ss ...[]string) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s...)
	}
	return out
}
