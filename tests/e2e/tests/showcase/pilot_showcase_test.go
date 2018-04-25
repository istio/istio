package showcase

import (
	"testing"
	"istio.io/istio/pkg/test"
)

func TestHTTPWithMTLS(t *testing.T) {
	test.Requires(t, environment.Apps, environment.MTLS)

	a := pilot.Apps.Get("a")
	t := pilot.Apps.Get("t")

	reqInfo := a.Call(t)

	// Verify that the request was received by t
	if err := t.Expect(reqInfo); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPWithMTLSAndMixer(t *testing.T) {
	test.Requires(t, environment.Mixer, environment.Pilot, environment.Apps, environment.MTLS)

	env := environment.Get()
	env.Configure(cfg)

	p := env.GetPilot()

	m := env.GetMixer()

	a := env.GetApp("a")
	t := env.GetApp("t")

	reqInfo := a.Call(t)

	// Verify that the request was received by t
	if err := t.Expect(reqInfo); err != nil {
		t.Fatal(err)
	}

	err := m.Expect(`
CHECK: "a" ....
`)
	if err != nil {
		t.Fatal(err)
	}
}