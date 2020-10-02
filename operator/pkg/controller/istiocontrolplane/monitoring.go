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

package istiocontrolplane

import "istio.io/pkg/monitoring"

var (
	metricOperatorReconcileCount = monitoring.NewSum(
		"operator_reconcile_count",
		"Count number of reconciliations done by operator",
		monitoring.WithLabels(monitoring.MustCreateLabel("completion_status")),
	)

	metricOperatorCustomResourceDeletions = monitoring.NewSum(
		"operator_cr_deletions",
		"Count number of IstioOperator CR deletions",
	)

	metricOperatorReconcileTime = monitoring.NewDistribution(
		"operator_reconcile_time",
		"reconcile time distribution in seconds",
		[]float64{0.0},
		monitoring.WithUnit(monitoring.Seconds),
	)
)

func init() {
	monitoring.MustRegister(
		metricOperatorReconcileCount,
		metricOperatorCustomResourceDeletions,
		metricOperatorReconcileTime,
	)
}
