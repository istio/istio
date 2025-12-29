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

package metrics

import (
	"strings"

	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/slices"
)

var charReplacer = strings.NewReplacer("/", "_", ".", "_", " ", "_", "-", "")

func promName(metricName string) string {
	s := strings.TrimPrefix(metricName, "/")
	return charReplacer.Replace(s)
}

func ExportedMetrics() []monitoring.MetricDefinition {
	return slices.Map(monitoring.ExportMetricDefinitions(), func(e monitoring.MetricDefinition) monitoring.MetricDefinition {
		e.Name = promName(e.Name)
		return e
	})
}
