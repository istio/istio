package istio

import (
	"fmt"
	"testing"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/util"
)

func TestMeshNetworkConfig(t *testing.T) {
	v1alpha1
	meshNetworks := v1alpha1.Values{
		Networks: map[string]*v1alpha1.Network{
			"network1": {
				Endpoints: []*v1alpha1.Network_NetworkEndpoints{
					{
						Ne: &v1alpha1.Network_NetworkEndpoints_FromRegistry{
							FromRegistry: "cluster1",
						},
					},
				},
				Gateways: []*v1alpha1.Network_IstioNetworkGateway{
					{
						Gw: &v1alpha1.Network_IstioNetworkGateway_RegistryServiceName{
							RegistryServiceName: "ingressgwplez",
						},
						Port: 443,
					},
				},
			},
		},
	}
	out := util.ToYAMLWithJSONPB(&meshNetworks)
	fmt.Println(out)
}
