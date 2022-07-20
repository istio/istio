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
