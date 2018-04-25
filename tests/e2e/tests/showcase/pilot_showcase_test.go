package showcase

import (
	"testing"
	"istio.io/istio/pkg/test"
)

func TestMTLSReachability(t *testing.T) {
	test.Requires(t, pilot.Apps, environment.MTLS)

	a := pilot.Apps.Get("a")
	t := pilot.Apps.Get("t")

	reqInfo := a.Call(t)

	// Verify that the request was received by t
	if err := t.Expect(reqInfo); err != nil {
		t.Fatal(err)
	}
}
