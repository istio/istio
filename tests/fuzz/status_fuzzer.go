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

package fuzz

import (
	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/api/meta/v1alpha1"
	"istio.io/istio/pilot/pkg/status/distribution"
)

// FuzzReconcileStatuses implements a fuzzer that targets
// status.ReconcileStatuses. It does so by inserting
// pseudo-random vlues in the config and the progress
// as well as pass a pseudo-random generation parameter.
func FuzzReconcileStatuses(data []byte) int {
	f := fuzz.NewConsumer(data)
	current := &v1alpha1.IstioStatus{}
	err := f.GenerateStruct(current)
	if err != nil {
		return 0
	}
	desired := distribution.Progress{}
	err = f.GenerateStruct(&desired)
	if err != nil {
		return 0
	}
	_, _ = distribution.ReconcileStatuses(current, desired)
	return 1
}
