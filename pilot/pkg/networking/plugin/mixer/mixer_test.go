package mixer_test

import (
	"fmt"
	"testing"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/gogo/protobuf/types"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/config/monitor/fakes"
	"istio.io/istio/pilot/pkg/model"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/plugin/mixer"
	"istio.io/istio/pilot/pkg/serviceregistry/cloudfoundry"
)

func TestOnOutboundRouteConfiguration(t *testing.T) {
	testRoute := istio_route.BuildDefaultHTTPRoute("outbound|8080|testservice.svc.cluster.local|testi.xyz.com", "testOps")

	outboundVHost := route.VirtualHost{
		Name:    fmt.Sprintf("%s|http|%d", model.TrafficDirectionOutbound, 9999),
		Domains: []string{"xyz.com"},
		Routes:  []route.Route{*testRoute},
	}

	vHost := []route.VirtualHost{outboundVHost}
	r := &xdsapi.RouteConfiguration{
		Name:             "test",
		VirtualHosts:     vHost,
		ValidateClusters: &types.BoolValue{Value: false},
	}

	mockClient := &fakes.CopilotClient{}

	serviceDiscovery := &cloudfoundry.ServiceDiscovery{
		Client:      mockClient,
		ServicePort: 8080,
	}
	environment := &model.Environment{
		Mesh: &meshconfig.MeshConfig{
			MixerCheckServer:  "istio-policy.test-system.svc.cluster.local:15444",
			MixerReportServer: "istio-telemetry.test-system.svc.cluster.local:15666",
		},
		ServiceDiscovery: serviceDiscovery,
	}
	inputParams := &plugin.InputParams{
		Env:              environment,
		ListenerProtocol: plugin.ListenerProtocolHTTP,
		Node: &model.Proxy{
			Type: model.Sidecar,
		},
		Port: &model.Port{
			Name: "http-foo",
			Port: 80,
		},
	}

	mixerPluging := mixer.NewPlugin()
	mixerPluging.OnOutboundRouteConfiguration(inputParams, r)
	if len(r.VirtualHosts[0].Routes[0].PerFilterConfig) == 0 {
		t.Error("expected PerFilterConfig on virtual hosts not be empty")
	}
}

func TestOnOutboundRouteConfigurationNoMixerServer(t *testing.T) {
	testRoute := istio_route.BuildDefaultHTTPRoute("outbound|8080|testservice.svc.cluster.local|testi.xyz.com", "testOps")

	outboundVHost := route.VirtualHost{
		Name:    fmt.Sprintf("%s|http|%d", model.TrafficDirectionOutbound, 9999),
		Domains: []string{"xyz.com"},
		Routes:  []route.Route{*testRoute},
	}

	vHost := []route.VirtualHost{outboundVHost}
	r := &xdsapi.RouteConfiguration{
		Name:             "test",
		VirtualHosts:     vHost,
		ValidateClusters: &types.BoolValue{Value: false},
	}

	mockClient := &fakes.CopilotClient{}

	serviceDiscovery := &cloudfoundry.ServiceDiscovery{
		Client:      mockClient,
		ServicePort: 8080,
	}
	environment := &model.Environment{
		Mesh: &meshconfig.MeshConfig{
			MixerCheckServer:  "",
			MixerReportServer: "",
		},
		ServiceDiscovery: serviceDiscovery,
	}
	inputParams := &plugin.InputParams{
		Env:              environment,
		ListenerProtocol: plugin.ListenerProtocolHTTP,
		Node: &model.Proxy{
			Type: model.Sidecar,
		},
		Port: &model.Port{
			Name: "http-foo",
			Port: 80,
		},
	}

	mixerPluging := mixer.NewPlugin()
	mixerPluging.OnOutboundRouteConfiguration(inputParams, r)
	if len(r.VirtualHosts[0].Routes[0].PerFilterConfig) != 0 {
		t.Error("expected PerFilterConfig on virtual hosts to be empty")
	}
}
