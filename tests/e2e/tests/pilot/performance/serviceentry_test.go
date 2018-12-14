package performance

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/google/uuid"
	"github.com/onsi/gomega"
	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	mixerEnv "istio.io/istio/mixer/test/client/env"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	mcpserver "istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/test/env"
	mockmcp "istio.io/istio/tests/e2e/tests/pilot/mock/mcp"
	"istio.io/istio/tests/util"
)

var (
	serviceEntryCount     = flag.Int("count", 1000, "Number of Service Entries to register")
	configRefreshInterval = flag.Int("interval", 30, "Number seconds to regenerate a new set of Service Entries")
	terminateAfter        = flag.Int("after", 240, "Number of seconds to run the test")
)

const (
	pilotDebugPort = 5555
	pilotGrpcPort  = 15010
	serviceDomain  = "example.com"
)

func TestServiceEntry(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	test := newTestParam(*serviceEntryCount, *configRefreshInterval, *terminateAfter)
	go test.init()

	t.Log("building & starting mock mcp server...")
	mcpServer, err := runMcpServer(test)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer mcpServer.Close()

	tearDown := initLocalPilotTestEnv(t, mcpServer.Port, pilotGrpcPort, pilotDebugPort)
	defer tearDown()

	t.Log("run edge router envoy...")
	gateway := runEnvoy(t, pilotGrpcPort, pilotDebugPort)
	defer gateway.TearDown()

	t.Log("check for service entries in debug endpoint...")
	g.Eventually(func() (int, error) {
		return registeredServiceEntries(fmt.Sprintf("http://127.0.0.1:%d/debug/configz", pilotDebugPort))
	}, "30s", "1s").Should(gomega.Equal(test.configCount))

	test.terminate()
}

type testParam struct {
	configCount       int
	interval          time.Duration
	terminateAfter    time.Duration
	cachedResponseMux sync.RWMutex
	cachedResponse    []*mcp.Envelope
	quit              chan struct{}
}

func newTestParam(count, interval, after int) *testParam {
	t := &testParam{
		configCount:    count,
		interval:       time.Duration(interval),
		terminateAfter: time.Duration(after),
		quit:           make(chan struct{}),
	}
	t.generateServiceEntries()
	return t
}

func (t *testParam) init() {
	// this is to create a slightly more realistic scenario by not
	// overwhelming the client with new service entries on every watch call
	ticker := time.NewTicker(time.Second * t.interval)
	for {
		select {
		case <-ticker.C:
			// update cache
			t.generateServiceEntries()
		case <-t.quit:
			ticker.Stop()
			return
		}
	}
}

func (t *testParam) terminate() {
	<-time.After(time.Second * t.terminateAfter)
	t.quit <- struct{}{}
}

func (t *testParam) mcpServerResponse(req *mcp.MeshConfigRequest) (*mcpserver.WatchResponse, mcpserver.CancelWatchFunc) {
	var cancelFunc mcpserver.CancelWatchFunc
	cancelFunc = func() {
		log.Printf("watch canceled for %s\n", req.GetTypeUrl())
	}
	// registering a wildcard gateway so that we can at least test some of the service entries
	// to see if they are registered
	if req.GetTypeUrl() == fmt.Sprintf("type.googleapis.com/%s", model.Gateway.MessageName) {
		return generateGateway(req), cancelFunc
	}
	if req.GetTypeUrl() == fmt.Sprintf("type.googleapis.com/%s", model.ServiceEntry.MessageName) {
		return &mcpserver.WatchResponse{
			Version:   req.GetVersionInfo(),
			TypeURL:   req.GetTypeUrl(),
			Envelopes: t.cachedResponses(),
		}, cancelFunc
	}

	return &mcpserver.WatchResponse{
		Version:   req.GetVersionInfo(),
		TypeURL:   req.GetTypeUrl(),
		Envelopes: []*mcp.Envelope{},
	}, cancelFunc
}

func (t *testParam) cachedResponses() []*mcp.Envelope {
	t.cachedResponseMux.RLock()
	defer t.cachedResponseMux.RUnlock()
	return t.cachedResponse
}

func (t *testParam) generateServiceEntries() (envelopes []*mcp.Envelope) {
	servicePorts := availablePorts(t.configCount)
	backendPorts := availablePorts(t.configCount)
	createTime := createTime()

	t.cachedResponseMux.Lock()
	for i := 0; i < t.configCount; i++ {
		host := fmt.Sprintf("%s.%s", randName(), serviceDomain)
		envelopes = append(envelopes, &mcp.Envelope{
			Metadata: &mcp.Metadata{
				Name:       fmt.Sprintf("serviceEntry-%s", randName()),
				CreateTime: createTime,
			},
			Resource: &types.Any{
				TypeUrl: fmt.Sprintf("type.googleapis.com/%s", model.ServiceEntry.MessageName),
				Value:   marshaledServiceEntry(servicePorts[i], backendPorts[i], host),
			},
		})

	}
	t.cachedResponse = envelopes
	t.cachedResponseMux.Unlock()
	return envelopes
}

func marshaledServiceEntry(servicePort, backendPort int, host string) []byte {
	serviceEntry := &networking.ServiceEntry{
		Hosts: []string{host},
		Ports: []*networking.Port{
			{
				Name:     "http",
				Number:   uint32(servicePort),
				Protocol: "http",
			},
		},
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Resolution: networking.ServiceEntry_STATIC,
		Endpoints: []*networking.ServiceEntry_Endpoint{
			{
				Address: "127.0.0.1",
				Ports: map[string]uint32{
					"http": uint32(backendPort),
				},
				Labels: map[string]string{"label": randName()},
			},
		},
	}

	se, err := proto.Marshal(serviceEntry)
	if err != nil {
		log.Fatalf("marshaling service entry %s\n", err)
	}
	return se
}

func generateGateway(req *mcp.MeshConfigRequest) *mcpserver.WatchResponse {
	return &mcpserver.WatchResponse{
		Version: req.GetVersionInfo(),
		TypeURL: req.GetTypeUrl(),
		Envelopes: []*mcp.Envelope{
			{
				Metadata: &mcp.Metadata{
					Name:       fmt.Sprintf("gateway-%s", randName()),
					CreateTime: createTime(),
				},
				Resource: &types.Any{
					TypeUrl: req.GetTypeUrl(),
					Value:   marshaledGateway(),
				},
			},
		},
	}
}

func marshaledGateway() []byte {
	gateway := &networking.Gateway{
		Servers: []*networking.Server{
			&networking.Server{
				Port: &networking.Port{
					Name:     "http-8099",
					Number:   8099,
					Protocol: "http",
				},
				Hosts: []string{"*"},
			},
		},
	}

	gw, err := proto.Marshal(gateway)
	if err != nil {
		log.Fatalf("marshaling gateway %s\n", err)
	}
	return gw

}

func createTime() *types.Timestamp {
	timeStamp, err := types.TimestampProto(time.Now())
	if err != nil {
		log.Fatalf("creating proto timestamp %s\n", err)
	}
	return timeStamp
}
func randName() string {
	name := uuid.New()
	return name.String()
}

func availablePorts(n int) (ports []int) {
	for i := 0; i < n; i++ {
		addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
		if err != nil {
			log.Fatalf("generating ports %s\n", err)
		}

		l, err := net.ListenTCP("tcp", addr)
		if err != nil {
			log.Fatalf("generating ports %s\n", err)
		}
		defer l.Close()
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports
}

func initLocalPilotTestEnv(t *testing.T, mcpPort, grpcPort, debugPort int) util.TearDownFunc {
	mixerEnv.NewTestSetup(mixerEnv.PilotMCPTest, t)
	debugAddr := fmt.Sprintf("127.0.0.1:%d", debugPort)
	grpcAddr := fmt.Sprintf("127.0.0.1:%d", grpcPort)
	_, tearDown := util.EnsureTestServer(addMcpAddrs(mcpPort), setupPilotDiscoveryHTTPAddr(debugAddr), setupPilotDiscoveryGrpcAddr(grpcAddr))
	return tearDown
}

func runMcpServer(param *testParam) (*mockmcp.Server, error) {
	supportedTypes := make([]string, len(model.IstioConfigTypes))
	for i, m := range model.IstioConfigTypes {
		supportedTypes[i] = fmt.Sprintf("type.googleapis.com/%s", m.MessageName)
	}

	server, err := mockmcp.NewServer("127.0.0.1:", supportedTypes, param.mcpServerResponse)
	if err != nil {
		return nil, err
	}

	return server, nil
}

func runEnvoy(t *testing.T, grpcPort, debugPort uint16) *mixerEnv.TestSetup {
	t.Log("create a new envoy test environment")
	tmpl, err := ioutil.ReadFile(env.IstioSrc + "/tests/testdata/cf_bootstrap_tmpl.json")
	if err != nil {
		t.Fatal("Can't read bootstrap template", err)
	}
	nodeIDGateway := "router~x~x~x"

	gateway := mixerEnv.NewTestSetup(25, t)
	gateway.SetNoMixer(true)
	gateway.SetNoProxy(true)
	gateway.SetNoBackend(true)
	gateway.IstioSrc = env.IstioSrc
	gateway.IstioOut = env.IstioOut
	gateway.Ports().PilotGrpcPort = grpcPort
	gateway.Ports().PilotHTTPPort = debugPort
	gateway.EnvoyConfigOpt = map[string]interface{}{
		"NodeID": nodeIDGateway,
	}
	gateway.EnvoyTemplate = string(tmpl)
	gateway.EnvoyParams = []string{
		"--service-node", nodeIDGateway,
		"--service-cluster", "x",
	}
	if err := gateway.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	return gateway
}

func addMcpAddrs(mcpPort int) func(*bootstrap.PilotArgs) {
	return func(arg *bootstrap.PilotArgs) {
		arg.MCPServerAddrs = []string{fmt.Sprintf("mcp://127.0.0.1:%d", mcpPort)}
	}
}

func setupPilotDiscoveryHTTPAddr(http string) func(*bootstrap.PilotArgs) {
	return func(arg *bootstrap.PilotArgs) {
		arg.DiscoveryOptions.HTTPAddr = http
	}
}

func setupPilotDiscoveryGrpcAddr(grpc string) func(*bootstrap.PilotArgs) {
	return func(arg *bootstrap.PilotArgs) {
		arg.DiscoveryOptions.GrpcAddr = grpc
	}
}

func registeredServiceEntries(apiEndpoint string) (int, error) {
	resp, err := http.DefaultClient.Get(apiEndpoint)
	if err != nil {
		return 0, err
	}
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	debug := string(respBytes)
	return strings.Count(debug, serviceDomain), nil
}
