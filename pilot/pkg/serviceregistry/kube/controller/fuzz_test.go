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

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/fuzz"
	"istio.io/istio/pkg/network"
)

func FuzzKubeController(f *testing.F) {
	fuzz.BaseCases(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		fg := fuzz.New(t, data)
		networkID := network.ID("fakeNetwork")
		fco := fuzz.Struct[FakeControllerOptions](fg)
		fco.SkipRun = true
		controller, _ := NewFakeControllerWithOptions(t, fco)
		controller.network = networkID

		p := fuzz.Struct[*corev1.Pod](fg)
		controller.pods.onEvent(p, model.EventAdd)
		s := fuzz.Struct[*corev1.Service](fg)
		controller.onServiceEvent(s, model.EventAdd)
		e := fuzz.Struct[*corev1.Endpoints](fg)
		controller.endpoints.onEvent(e, model.EventAdd)
	})
}
