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

package helm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	operatortest "istio.io/istio/operator/pkg/test"
	"istio.io/istio/operator/pkg/values"
	tutil "istio.io/istio/pilot/test/util"
)

type testCase struct {
	desc        string
	releaseName string
	namespace   string
	chartName   string
	diffSelect  string
}

func TestRender(t *testing.T) {
	cases := []testCase{
		{
			desc:        "gateway-deployment",
			releaseName: "istio-ingress",
			namespace:   "istio-ingress",
			chartName:   "gateway",
			diffSelect:  "Deployment:*:istio-ingress",
		},
		{
			desc:        "gateway-env-var-from",
			releaseName: "istio-ingress",
			namespace:   "istio-ingress",
			chartName:   "gateway",
			diffSelect:  "Deployment:*:istio-ingress",
		},
		{
			desc:        "gateway-additional-containers",
			releaseName: "istio-ingress",
			namespace:   "istio-ingress",
			chartName:   "gateway",
			diffSelect:  "Deployment:*:istio-ingress",
		},
		{
			desc:        "gateway-init-containers",
			releaseName: "istio-ingress",
			namespace:   "istio-ingress",
			chartName:   "gateway",
			diffSelect:  "Deployment:*:istio-ingress",
		},
		{
			desc:        "istiod-traffic-distribution",
			releaseName: "istiod",
			namespace:   "istio-system",
			chartName:   "istio-control/istio-discovery",
			diffSelect:  "Service:*:istiod",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			inPath := filepath.Join("testdata", "input", tc.desc+".yaml")
			data, err := os.ReadFile(inPath)
			if err != nil {
				require.NoError(t, err)
			}
			var vals values.Map
			if err := yaml.Unmarshal(data, &vals); err != nil {
				t.Fatalf("error %s: %s", err, inPath)
			}
			m, _, err := Render(tc.releaseName, tc.namespace, tc.chartName, vals, nil)
			require.NoError(t, err)

			b := strings.Builder{}
			for _, mf := range m {
				b.WriteString(mf.Content)
				b.WriteString("\n---\n") // yaml separator
			}
			got := b.String()
			if len(tc.diffSelect) > 0 {
				got = operatortest.FilterManifest(t, got, tc.diffSelect)
			}

			if len(tc.diffSelect) == 0 {
				t.Skip("skipping test that has no diff select")
			}

			outPath := filepath.Join("testdata", "output", tc.desc+".golden.yaml")
			tutil.RefreshGoldenFile(t, []byte(got), outPath)

			want := string(tutil.ReadFile(t, outPath))
			if got != want {
				t.Fatal(cmp.Diff(got, want))
			}
		})
	}
}
