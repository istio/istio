package v2_test

import (
	"context"
	"log"
	"testing"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"google.golang.org/grpc"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/tests/util"
)

// Make a direct EDS grpc request to pilot, verify the result is as expected.
func directRequest(t *testing.T) {
	conn, err := grpc.Dial(util.MockPilotGrpcAddr, grpc.WithInsecure())
	if err != nil {
		t.Fatal("Connection failed", err)
	}

	xds := xdsapi.NewEndpointDiscoveryServiceClient(conn)
	edsstr, err := xds.StreamEndpoints(context.Background())
	if err != nil {
		t.Fatal("Rpc failed", err)
	}

	err = edsstr.Send(&xdsapi.DiscoveryRequest{
		ResourceNames: []string{"hello.default.svc.cluster.local|http"}})
	if err != nil {
		t.Fatal("Send failed", err)
	}

	res1, err := edsstr.Recv()
	if err != nil {
		t.Fatal("Recv failed", err)
	}

	if res1.TypeUrl != "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment" {
		t.Error("Expecting type.googleapis.com/envoy.api.v2.ClusterLoadAssignment got ", res1.TypeUrl)
	}
	if res1.Resources[0].TypeUrl != "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment" {
		t.Error("Expecting type.googleapis.com/envoy.api.v2.ClusterLoadAssignment got ", res1.Resources[0].TypeUrl)
	}
	cla := &xdsapi.ClusterLoadAssignment{}
	err = cla.Unmarshal(res1.Resources[0].Value)
	if err != nil {
		t.Fatal("Failed to parse proto ", err)
	}
	// TODO: validate VersionInfo and nonce once we settle on a scheme

	t.Log(res1.String())
	t.Log(cla.String())

	// This should happen in 15 seconds, for the periodic refresh
	// TODO: verify push works
	res1, err = edsstr.Recv()
	if err != nil {
		t.Fatal("Recv2 failed", err)
	}
	t.Log(res1.String())

	edsstr.CloseSend()
}

func TestEds(t *testing.T) {
	server := util.EnsureTestServer()

	server.MemoryServiceDiscovery.AddService("mysvc", &model.Service{

	})

	// Verify services are set
	srv, err := server.ServiceController.Services()
	if err != nil {
		t.Fatal("Starting pilot", err)
	}
	log.Println(srv)

	go startEnvoy()

	t.Run("DirectRequest", func(t *testing.T) {
		directRequest(t)
	})
}

func startEnvoy() error {
	err := util.RunEnvoy("xds", "tests/testdata/envoy_local.json")
	if err != nil {
		return err
	}

	return nil
}
