package leaderelection

import (
	"testing"

	"github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/features"
)

func TestBuildClusterScopedLeaderElectionConfigName(t *testing.T) {
	features.ClusterName = "gw-123-istio"
	testCases := []struct {
		input  string
		expect string
	}{
		{
			input:  GatewayStatusController,
			expect: "gw-123-istio-gateway-status-leader",
		},
		{
			input:  GatewayDeploymentController,
			expect: "gw-123-istio-gateway-deployment",
		},
		{
			input:  IngressController,
			expect: "gw-123-istio-leader",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.input, func(t *testing.T) {
			g := gomega.NewWithT(t)
			result := BuildClusterScopedLeaderElection(testCase.input)
			g.Expect(result).To(gomega.Equal(testCase.expect))
		})
	}
}
