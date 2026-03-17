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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"sigs.k8s.io/yaml"

	"istio.io/istio/istioctl/pkg/install/k8sversion"
	"istio.io/istio/manifests"
	"istio.io/istio/operator/pkg/manifest"
	operatortest "istio.io/istio/operator/pkg/test"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/values"
	tutil "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/test/util/yml"
)

type testCase struct {
	desc        string
	releaseName string
	namespace   string
	chartName   string
	diffSelect  string
	isUpgrade   bool
}

func renderWithOptions(releaseName, namespace, directory string, iop values.Map, isUpgrade bool) ([]manifest.Manifest, util.Errors, error) {
	vals, _ := iop.GetPathMap("spec.values")
	installPackagePath := iop.GetPathString("spec.installPackagePath")
	f := manifests.BuiltinOrDir(installPackagePath)
	path := pathJoin("charts", directory)
	chrt, err := loadChart(f, path)
	if err != nil {
		return nil, nil, fmt.Errorf("load chart: %v", err)
	}

	options := chartutil.ReleaseOptions{
		Name:      releaseName,
		Namespace: namespace,
		IsUpgrade: isUpgrade,
		IsInstall: !isUpgrade,
	}

	caps := *chartutil.DefaultCapabilities
	operatorVersion, _ := chartutil.ParseKubeVersion("1." + strconv.Itoa(k8sversion.MinK8SVersion) + ".0")
	caps.KubeVersion = *operatorVersion

	helmVals, err := chartutil.ToRenderValues(chrt, vals, options, &caps)
	if err != nil {
		return nil, nil, fmt.Errorf("converting values: %v", err)
	}

	files, err := engine.Render(chrt, helmVals)
	if err != nil {
		return nil, nil, err
	}

	var warnings Warnings
	keys := make([]string, 0, len(files))
	for k, v := range files {
		if strings.HasSuffix(k, NotesFileNameSuffix) {
			warnings = extractWarnings(v)
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	results := make([]string, 0, len(keys))
	for _, k := range keys {
		results = append(results, yml.SplitString(files[k])...)
	}

	mfs, err := manifest.Parse(results)
	return mfs, warnings, err
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
			desc:        "gateway-service-selector-labels",
			releaseName: "istio-ingress",
			namespace:   "istio-ingress",
			chartName:   "gateway",
			diffSelect:  "Service:*:istio-ingress",
		},
		{
			desc:        "istiod-traffic-distribution",
			releaseName: "istiod",
			namespace:   "istio-system",
			chartName:   "istio-control/istio-discovery",
			diffSelect:  "Service:*:istiod",
		},
		{
			desc:        "istiod-pdb-default",
			releaseName: "istiod",
			namespace:   "istio-system",
			chartName:   "istio-control/istio-discovery",
			diffSelect:  "PodDisruptionBudget:*:istiod",
		},
		{
			desc:        "istiod-pdb-max-unavailable",
			releaseName: "istiod",
			namespace:   "istio-system",
			chartName:   "istio-control/istio-discovery",
			diffSelect:  "PodDisruptionBudget:*:istiod",
		},
		{
			desc:        "istiod-pdb-unhealthy-pod-eviction-policy",
			releaseName: "istiod",
			namespace:   "istio-system",
			chartName:   "istio-control/istio-discovery",
			diffSelect:  "PodDisruptionBudget:*:istiod",
		},
		{
			desc:        "istiod-pdb-2replicas",
			releaseName: "istiod",
			namespace:   "istio-system",
			chartName:   "istio-control/istio-discovery",
			diffSelect:  "PodDisruptionBudget:*:istiod",
		},
		{
			desc:        "istiod-pdb-autoscaleMin2",
			releaseName: "istiod",
			namespace:   "istio-system",
			chartName:   "istio-control/istio-discovery",
			diffSelect:  "PodDisruptionBudget:*:istiod",
		},
		{
			desc:        "gateway-pdb-default",
			releaseName: "istio-ingress",
			namespace:   "istio-ingress",
			chartName:   "gateway",
			diffSelect:  "PodDisruptionBudget:*:istio-ingress",
		},
		{
			desc:        "gateway-pdb-2replicas",
			releaseName: "istio-ingress",
			namespace:   "istio-ingress",
			chartName:   "gateway",
			diffSelect:  "PodDisruptionBudget:*:istio-ingress",
		},
		{
			desc:        "gateway-pdb-autoscaleMin2",
			releaseName: "istio-ingress",
			namespace:   "istio-ingress",
			chartName:   "gateway",
			diffSelect:  "PodDisruptionBudget:*:istio-ingress",
		},
		{
			desc:        "istiod-webhook-install",
			releaseName: "istiod",
			namespace:   "istio-system",
			chartName:   "istio-control/istio-discovery",
			diffSelect:  "ValidatingWebhookConfiguration:*:istio-validator-istio-system",
		},
		{
			desc:        "istiod-webhook-upgrade",
			releaseName: "istiod",
			namespace:   "istio-system",
			chartName:   "istio-control/istio-discovery",
			diffSelect:  "ValidatingWebhookConfiguration:*:istio-validator-istio-system",
			isUpgrade:   true,
		},
		{
			desc:        "istiod-webhook-failure-policy",
			releaseName: "istiod",
			namespace:   "istio-system",
			chartName:   "istio-control/istio-discovery",
			diffSelect:  "ValidatingWebhookConfiguration:*:istio-validator-istio-system",
		},
		{
			desc:        "base-webhook-install",
			releaseName: "istio-base",
			namespace:   "istio-system",
			chartName:   "base",
			diffSelect:  "ValidatingWebhookConfiguration:*:istiod-default-validator",
		},
		{
			desc:        "base-webhook-upgrade",
			releaseName: "istio-base",
			namespace:   "istio-system",
			chartName:   "base",
			diffSelect:  "ValidatingWebhookConfiguration:*:istiod-default-validator",
			isUpgrade:   true,
		},
		{
			desc:        "base-webhook-failure-policy",
			releaseName: "istio-base",
			namespace:   "istio-system",
			chartName:   "base",
			diffSelect:  "ValidatingWebhookConfiguration:*:istiod-default-validator",
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
			m, _, err := renderWithOptions(tc.releaseName, tc.namespace, tc.chartName, vals, tc.isUpgrade)
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
