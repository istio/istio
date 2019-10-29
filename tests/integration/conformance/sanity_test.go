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

package conformance

import (
	"testing"

	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/pkg/test/framework"
)

// The following collections in metadata are ignored.
var ignoredCollections = []string{
	// Standard K8s resources are used internally
	"k8s/core/v1/nodes",
	"k8s/core/v1/pods",
	"k8s/core/v1/services",
	"k8s/core/v1/namespaces",
	"k8s/core/v1/endpoints",
	"k8s/extensions/v1beta1/ingresses",

	// The legacy Mixer types are no longer supported. We should remove them after deprecation cycle.
	"istio/config/v1alpha2/legacy/cloudwatches",
	"istio/config/v1alpha2/legacy/statsds",
	"istio/config/v1alpha2/legacy/stdios",
	"istio/config/v1alpha2/legacy/listentries",
	"istio/config/v1alpha2/legacy/metrics",
	"istio/config/v1alpha2/legacy/stackdrivers",
	"istio/config/v1alpha2/legacy/kuberneteses",
	"istio/config/v1alpha2/legacy/quotas",
	"istio/config/v1alpha2/legacy/zipkins",
	"istio/config/v1alpha2/legacy/prometheuses",
	"istio/config/v1alpha2/legacy/redisquotas",
	"istio/config/v1alpha2/legacy/reportnothings",
	"istio/config/v1alpha2/legacy/edges",
	"istio/config/v1alpha2/legacy/noops",
	"istio/config/v1alpha2/legacy/signalfxs",
	"istio/config/v1alpha2/legacy/solarwindses",
	"istio/config/v1alpha2/legacy/apikeys",
	"istio/config/v1alpha2/legacy/bypasses",
	"istio/config/v1alpha2/legacy/dogstatsds",
	"istio/config/v1alpha2/legacy/kubernetesenvs",
	"istio/config/v1alpha2/legacy/listcheckers",
	"istio/config/v1alpha2/legacy/tracespans",
	"istio/config/v1alpha2/legacy/authorizations",
	"istio/config/v1alpha2/legacy/fluentds",
	"istio/config/v1alpha2/legacy/memquotas",
	"istio/config/v1alpha2/legacy/opas",
	"istio/config/v1alpha2/legacy/checknothings",
	"istio/config/v1alpha2/legacy/circonuses",
	"istio/config/v1alpha2/legacy/deniers",
	"istio/config/v1alpha2/legacy/logentries",
	"istio/config/v1alpha2/legacy/rbacs",
}

func TestMissingMCPTests(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			collections := make(map[string]struct{})
			for _, col := range metadata.MustGet().AllCollectionsInSnapshots(metadata.SnapshotNames()) {
				collections[col] = struct{}{}
			}

			cases, err := loadCases()
			if err != nil {
				t.Fatalf("Unable to load test cases: %v", err)
			}

			tested := make(map[string]struct{})
			for _, c := range cases {
				for _, s := range c.Stages {
					if s.MCP == nil {
						return
					}

					for _, col := range s.MCP.Constraints {
						tested[col.Name] = struct{}{}
					}
				}
			}

			ignored := make(map[string]struct{})
			for _, i := range ignoredCollections {
				ignored[i] = struct{}{}
			}

			for i := range ignored {
				if _, ok := tested[i]; ok {
					ctx.Errorf("Ignored collection is tested. Please remove it from 'ignoredCollections': %s", i)
				}

				if _, ok := collections[i]; !ok {
					ctx.Errorf("Unknown collection is ignored. If this type is not served, please remove from ignore list: %s", i)
				}
			}

			for collection := range collections {
				if _, found := ignored[collection]; found {
					continue
				}

				if _, found := tested[collection]; !found {
					ctx.Errorf("MCP collection not tested: %q", collection)
				}
			}
		})
}
