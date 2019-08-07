package tests

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/istio.io/examples"
)

var (
	ist istio.Instance
)

func TestMain(m *testing.M) {
	framework.NewSuite("mtls-migration", m).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
		Run()
}

//https://istio.io/docs/tasks/security/mtls-migration/
//https://github.com/istio/istio.io/blob/release-1.2/content/docs/tasks/security/mtls-migration/index.md
func TestMTLS(t *testing.T) {
	ex := examples.New(t, "mtls-migration")
	ex.AddScript("create-ns-foo-bar.sh", examples.TextOutput)

	ex.AddScript("curl-foo-bar-legacy.sh", examples.TextOutput)

	//verify that all requests returns 200 ok

	ex.AddScript("verify-initial-policies.sh", examples.TextOutput)

	//verify that only the following exist:
	// NAMESPACE      NAME                          AGE
	// istio-system   grafana-ports-mtls-disabled   3m

	ex.AddScript("verify-initial-destinationrules.sh", examples.TextOutput)

	//verify that only the following exists:
	//NAMESPACE      NAME              AGE
	//istio-system   istio-policy      25m
	//istio-system   istio-telemetry   25m

	ex.AddScript("configure-mtls-destinationrule.sh", examples.TextOutput)
	ex.AddScript("curl-foo-bar-legacy.sh", examples.TextOutput)

	//verify 200ok from all requests

	ex.AddScript("httpbin-foo-mtls-only.sh", examples.TextOutput)
	ex.AddScript("curl-foo-bar-legacy.sh", examples.TextOutput)

	//verify 200 from first 2 requests and 503 from 3rd request

	ex.AddScript("cleanup.sh", examples.TextOutput)
	ex.Run()
}
