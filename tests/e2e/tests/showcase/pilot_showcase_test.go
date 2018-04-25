package showcase

import (
	"testing"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/environment"
)

var cfg = ""

func TestHTTPWithMTLS(t *testing.T) {
	test.Requires(t, environment.Apps, environment.Pilot, environment.MTLS)

	env := test.GetEnvironment(t)
	env.Configure(cfg)

	appa := env.GetApp("a")
	appt := env.GetApp("t")

	reqInfo := appa.Call(appt)

	// Verify that the request was received by t
	if err := appt.Expect(reqInfo); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPWithMTLSAndMixer(t *testing.T) {
	test.Requires(t, environment.Mixer, environment.Pilot, environment.Apps, environment.MTLS)

	env := test.GetEnvironment(t)

	env.Configure(cfg)

	_ = env.GetPilot()

	m := env.GetMixer()

	appa := env.GetApp("a")
	appt := env.GetApp("t")

	reqInfo := appa.Call(appt)

	// Verify that the request was received by t
	if err := appt.Expect(reqInfo); err != nil {
		t.Fatal(err)
	}

	err := m.Expect(`
CHECK: "a" ....
`)
	if err != nil {
		t.Fatal(err)
	}
}