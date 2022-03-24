package pilot

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/gateway-api/conformance/tests"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/scopes"
)

// GatewayConformanceInputs defines inputs to the gateway conformance test.
// The upstream build requires using `testing.T` types, which we cannot pass using our framework.
// To workaround this, we set up the inputs it TestMain.
type GatewayConformanceInputs struct {
	Client  kube.ExtendedClient
	Cleanup bool
}

var gatewayConformanceInputs GatewayConformanceInputs

func TestGatewayConformance(t *testing.T) {
	mapper, _ := gatewayConformanceInputs.Client.UtilFactory().ToRESTMapper()
	c, err := client.New(gatewayConformanceInputs.Client.RESTConfig(), client.Options{
		Scheme: kube.IstioScheme,
		Mapper: mapper,
	})
	if err != nil {
		t.Fatal(err)
	}

	csuite := suite.New(suite.Options{
		Client:           c,
		GatewayClassName: "istio",
		Debug:            scopes.Framework.DebugEnabled(),
		Cleanup:          gatewayConformanceInputs.Cleanup,
		RoundTripper:     nil,
	})
	csuite.Setup(t)

	for _, ct := range tests.ConformanceTests {
		t.Run(ct.ShortName, func(t *testing.T) {
			ct.Run(t, csuite)
		})
	}
}
