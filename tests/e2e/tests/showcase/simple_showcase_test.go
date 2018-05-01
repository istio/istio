package showcase

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/environment"
	"testing"
)

var cfg = ""

// Reimplement TestSvc2Svc in a_simple-1_test.go
func TestSvcLoading(t *testing.T) {
	// This Requires statement should ensure that all elements are in runnig states
	test.Requires(t, denpendency.Apps, dependency.Fortio, dependency.Pilot)

	env := test.GetEnvironment(t)
	env.Configure(cfg)
	fortioServer := env.GetFortio()
	serverURL := fortioServer.GetURL() + "/echo"
	apps := env.GetApp("echosrv")

	cmd := "load -qps 0 -t 10s " + serverURL
	// Test Loading
	for _, app := range apps {
		t.Run(url, func(t *testing.T) {
			if _, err := app.CallFotio(cmd); err != nil {
				t.Fatal("Failed to run fortio %s.", err)
			}
		})
	}
}
