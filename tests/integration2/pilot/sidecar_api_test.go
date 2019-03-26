package pilot

import (
	"fmt"
	"net"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdscore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/structpath"
)

func TestSidecarListeners(t *testing.T) {
	// Call Requires to explicitly initialize dependencies that the test needs.
	ctx := framework.GetContext(t)
	// TODO - remove prior to checkin
	scopes.Framework.SetOutputLevel(log.DebugLevel)

	// TODO: Limit to Native environment until the Kubernetes environment is supported in the Galley
	// component
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.NativeEnvironment)
	ctx.RequireOrFail(t, lifecycle.Test, &ids.Galley)
	ctx.RequireOrFail(t, lifecycle.Test, &ids.Pilot)

	// Get the port for mixer checks
	mixerCheckPort := components.GetMixer(ctx, t).GetCheckAddress().(*net.TCPAddr).Port
	pilot := components.GetPilot(ctx, t)

	// Simulate proxy identity of a sidecar ...
	nodeID := &model.Proxy{
		ClusterID:   "integration-test",
		Type:        model.SidecarProxy,
		IPAddresses: []string{"10.2.0.1"},
		ID:          "app3.testns",
		DNSDomain:   "testns.cluster.local",
	}

	// ... and get listeners from Pilot for that proxy
	req := &xdsapi.DiscoveryRequest{
		Node: &xdscore.Node{
			Id: nodeID.ServiceNode(),
		},
		TypeUrl: "type.googleapis.com/envoy.api.v2.Listener",
	}
	// Start the xDS stream
	err := pilot.StartDiscovery(req)
	if err != nil {
		t.Fatalf("Failed to test as no resource accepted: %v", err)
	}

	// Test the empty case where no config is loaded
	err = pilot.WatchDiscovery(time.Second*5,
		func(response *xdsapi.DiscoveryResponse) (b bool, e error) {
			validator := structpath.AssertThatProto(t, response)
			if !validator.Accept("{.resources[?(@.address.socketAddress.portValue==%v)]}", mixerCheckPort) {
				return false, nil
			}
			validateListenersNoConfig(t, validator, mixerCheckPort)
			return true, nil
		})
	if err != nil {
		t.Fatalf("Failed to test as no resource accepted: %v", err)
	}

	/*
		// TODO - Re-enable once Galley is wired into Pilot
		// Load the canonical dataset into Galley and by implication Pilot
		gal := components.GetGalley(ctx, t)
		// Apply some config
		path, err := filepath.Abs("../../testdata/config")
		if err != nil {
			t.Fatalf("No such directory: %v", err)
		}
		err = gal.ApplyConfigDir(path)
		if err != nil {
			t.Fatalf("Error applying directory: %v", err)
		}

		// Now continue to watch on the same stream
		err = pilot.WatchDiscovery(time.Second*5000,
			func(response *xdsapi.DiscoveryResponse) (b bool, e error) {
				validator := structpath.AssertThatProto(t, response)
				if !validator.Accept("{.resources[?(@.address.socketAddress.xxxValue==%v)]}", mixerCheckPort) {
					return false, nil
				}
				validateListenersNoConfig(t, validator, mixerCheckPort)
				return true, nil
			})
		if err != nil {
			t.Fatalf("Failed to test as no resource accepted: %v", err)
		}
	*/
}

/*
	resp, err := pilot.CallDiscovery(req)
	for i := 0; i < 10; i++ {
		 log.Debugf("Listener length %v", len(resp.Resources))
		 time.Sleep(time.Second * 2)
		  resp, err = pilot.CallDiscovery(req)
	}

	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}
*/

func validateListenersNoConfig(t *testing.T, response *structpath.Structpath, mixerCheckPort int) {
	t.Run("validate-mixer-listener", func(t *testing.T) {
		mixerListener := response.ForTest(t).
			Select("{.resources[?(@.address.socketAddress.portValue==%v)]}", mixerCheckPort)

		mixerListener.
			Equals("0.0.0.0", "{.address.socketAddress.address}").
			// Example doing a struct comparison, note the pain with oneofs....
			Equals(&xdscore.SocketAddress{
				Address: "0.0.0.0",
				PortSpecifier: &xdscore.SocketAddress_PortValue{
					PortValue: uint32(mixerCheckPort),
				},
			}, "{.address.socketAddress}").
			Select("{.filterChains[0].filters[0]}").
			Equals("envoy.http_connection_manager", "{.name}").
			Equals(true, "{.config.generate_request_id}").
			Equals("mixer envoy.cors envoy.fault envoy.router", "{.config.http_filters[*].name}").
			Select("{.config}").
			Exists("{.rds.config_source.ads}").
			Exists("{.stat_prefix}").
			Equals(100, "{.tracing.client_sampling.value}").
			Equals(100, "{.tracing.overall_sampling.value}").
			Equals(100, "{.tracing.random_sampling.value}").
			Equals("EGRESS", "{.tracing.operation_name}").
			Equals("websocket", "{.upgrade_configs[*].upgrade_type}").
			Equals(false, "{.use_remote_address}")

		mixerListener.
			Equals(false, "{.deprecatedV1.bindToPort}").
			NotExists("{.useOriginalDst}")

		mixerListener.
			Select("{.filterChains[0].filters[0].config.http_filters[?(@.name==\"mixer\")].config}").
			Equals("kubernetes://app3.testns", "{.forward_attributes.attributes['source.uid'].string_value}").
			Equals("testns", "{.mixer_attributes.attributes['source.namespace'].string_value}").
			Equals("outbound", "{.mixer_attributes.attributes['context.reporter.kind'].string_value}").
			Equals(true, "{.service_configs.default.disable_check_calls}").
			Equals(fmt.Sprintf("outbound|%v||mixer.istio-system.svc.local", mixerCheckPort), "{.transport.check_cluster}").
			Equals(fmt.Sprintf("outbound|%v||mixer.istio-system.svc.local", mixerCheckPort), "{.transport.report_cluster}").
			Equals("FAIL_CLOSE", "{.transport.network_fail_policy.policy}")

	})
	t.Run("iptables-forwarding-listener", func(t *testing.T) {
		response.ForTest(t).
			Select("{.resources[?(@.address.socketAddress.portValue==15001)]}").
			Equals("virtual", "{.name}").
			Equals("0.0.0.0", "{.address.socketAddress.address}").
			Equals("envoy.tcp_proxy", "{.filterChains[0].filters[*].name}").
			// Current default for egress is allowed ( based on user feedback ).
			// TODO: add test for blocked by default, based on setting.
			Equals("PassthroughCluster", "{.filterChains[0].filters[0].config.cluster}").
			Equals("PassthroughCluster", "{.filterChains[0].filters[0].config.stat_prefix}").
			Equals(true, "{.useOriginalDst}")
	})
}

// Capturing TestMain allows us to:
// - Do cleanup before exit
// - process testing specific flags
func TestMain(m *testing.M) {
	framework.Run("sidecar_api_test", m)
}
