//go:build integ
// +build integ

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

package telemetry

import (
	"context"

	"github.com/prometheus/common/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/prometheus"
)

// PromDiff compares a query with labels to a query of the same metric without labels, and notes the closest matching
// metric.
func PromDiff(t test.Failer, prom prometheus.Instance, cluster cluster.Cluster, query prometheus.Query) {
	t.Helper()
	unlabelled := prometheus.Query{Metric: query.Metric}
	v, _ := prom.Query(cluster, unlabelled)
	if v == nil {
		t.Logf("no metrics found for %v", unlabelled)
		return
	}
	switch v.Type() {
	case model.ValVector:
		value := v.(model.Vector)
		var allMismatches []map[string]string
		full := []model.Metric{}
		for _, s := range value {
			misMatched := map[string]string{}
			for k, want := range query.Labels {
				got := string(s.Metric[model.LabelName(k)])
				if want != got {
					misMatched[k] = got
				}
			}
			if len(misMatched) == 0 {
				continue
			}
			allMismatches = append(allMismatches, misMatched)
			full = append(full, s.Metric)
		}
		if len(allMismatches) == 0 {
			t.Logf("no diff found")
			return
		}
		t.Logf("query %q returned %v series, but none matched our query exactly.", query.Metric, len(value))
		t.Logf("Original query: %v", query.String())
		for i, m := range allMismatches {
			t.Logf("Series %d (source: %v/%v)", i, full[i]["namespace"], full[i]["pod"])
			missing := []string{}
			for k, v := range m {
				if v == "" {
					missing = append(missing, k)
				} else {
					t.Logf("  for label %q, wanted %q but got %q", k, query.Labels[k], v)
				}
			}
			if len(missing) > 0 {
				t.Logf("  missing labels: %v", missing)
			}
		}

	default:
		t.Fatalf("PromDiff expects Vector, got %v", v.Type())

	}
}

// PromDump gets all of the recorded values for a metric by name and generates a report of the values.
// used for debugging of failures to provide a comprehensive view of traffic experienced.
func PromDump(cluster cluster.Cluster, prometheus prometheus.Instance, query prometheus.Query) string {
	if value, err := prometheus.Query(cluster, query); err == nil {
		return value.String()
	}

	return ""
}

// Get trust domain of the cluster.
func GetTrustDomain(cluster cluster.Cluster, istioNamespace string) string {
	meshConfigMap, err := cluster.Kube().CoreV1().ConfigMaps(istioNamespace).Get(context.Background(), "istio", metav1.GetOptions{})
	defaultTrustDomain := mesh.DefaultMeshConfig().TrustDomain
	if err != nil {
		return defaultTrustDomain
	}

	configYaml, ok := meshConfigMap.Data["mesh"]
	if !ok {
		return defaultTrustDomain
	}

	cfg, err := mesh.ApplyMeshConfigDefaults(configYaml)
	if err != nil {
		return defaultTrustDomain
	}

	return cfg.TrustDomain
}
