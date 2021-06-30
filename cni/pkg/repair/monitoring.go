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

package repair

import (
	"istio.io/pkg/monitoring"
)

var (
	typeLabel  = monitoring.MustCreateLabel("type")
	deleteType = "delete"
	labelType  = "label"

	resultLabel   = monitoring.MustCreateLabel("result")
	resultSuccess = "success"
	resultSkip    = "skip"
	resultFail    = "fail"

	podsRepaired = monitoring.NewSum(
		"istio_cni_repair_pods_repaired_total",
		"Total number of pods repaired by repair controller",
		monitoring.WithLabels(typeLabel, resultLabel),
	)
)

func init() {
	monitoring.MustRegister(podsRepaired)
}
