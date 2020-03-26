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

	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schema/snapshots"
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
}

func TestMissingMCPTests(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			collections := make(map[string]struct{})
			for _, col := range schema.MustGet().AllCollectionsInSnapshots(snapshots.SnapshotNames()) {
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
