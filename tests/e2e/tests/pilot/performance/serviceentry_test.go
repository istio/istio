package performance

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
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
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/mcp/snapshot"
	mcptest "istio.io/istio/pkg/mcp/testing"
	"istio.io/istio/tests/util"
)

var (
	serviceEntryCount = flag.Int("count", 1000, "Number of Service Entries to register")
	terminateAfter    = flag.Int("after", 60, "Number of seconds to run the test")
	// TODO: configure number of envoys
)

const (
	pilotDebugPort = 5555
	pilotGrpcPort  = 15010
	serviceDomain  = "example.com"
)

func TestServiceEntry(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Log("building & starting mock mcp server...")
	mcpServer, err := runMcpServer()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer mcpServer.Close()

	go runSnapshot(mcpServer)

	tearDown := initLocalPilotTestEnv(t, mcpServer.Port, pilotGrpcPort, pilotDebugPort)
	defer tearDown()

	t.Log("run edge router envoy...")
	adsc := adsConnectAndWait(pilotGrpcPort)
	defer adsc.Close()

	t.Log("check for service entries in pilot's debug endpoint...")
	g.Eventually(func() (int, error) {
		return registeredServiceEntries(fmt.Sprintf("http://127.0.0.1:%d/debug/configz", pilotDebugPort))
	}, "30s", "1s").Should(gomega.Equal(*serviceEntryCount))

	t.Log("check for registered endpoints in envoys's debug endpoint...")
	g.Eventually(func() (int, error) {
		var data map[string]interface{}
		err = json.Unmarshal([]byte(adsc.EndpointsJSON()), &data)
		if err != nil {
			return 0, err
		}
		return len(data), nil
	}, "30s", "1s").Should(gomega.Equal(*serviceEntryCount))

	terminate()
}

func runSnapshot(mcpServer *mcptest.Server) {
	configInterval := time.NewTicker(time.Second * 3)
	v := 0
	b := snapshot.NewInMemoryBuilder()
	for {
		select {
		case <-configInterval.C:
			for {
				// wait for pilot to make a MCP request
				if status := mcpServer.Cache.Status(snapshot.DefaultGroup); status != nil {
					if status.Watches() > 0 {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			v++
			version := strconv.Itoa(v)
			for _, m := range model.IstioConfigTypes {
				if m.MessageName == model.ServiceEntry.MessageName {
					b.Set("type.googleapis.com/istio.networking.v1alpha3.ServiceEntry", version, generateServiceEntries())
				} else if m.MessageName == model.Gateway.MessageName {
					b.Set("type.googleapis.com/istio.networking.v1alpha3.Gateway", version, generateGateway())
				} else {
					b.Set(fmt.Sprintf("type.googleapis.com/%s", m.MessageName), version, []*mcp.Envelope{})
				}
			}

			lastSnapshot := time.Now()
			shot := b.Build()
			mcpServer.Cache.SetSnapshot(snapshot.DefaultGroup, shot)
			b = shot.Builder()

			// wait for client to ACK the pushed snapshot
			for {
				if status := mcpServer.Cache.Status(snapshot.DefaultGroup); status != nil {
					if status.LastWatchRequestTime().After(lastSnapshot) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
}

func terminate() {
	<-time.After(time.Second * time.Duration(*terminateAfter))
}

func adsConnectAndWait(grpcPort uint16) *adsc.ADSC {
	pilotAddr := fmt.Sprintf("127.0.0.1:%d", grpcPort)
	c, err := adsc.Dial(pilotAddr, "", &adsc.Config{
		IP: "127.0.0.1",
	})
	if err != nil {
		log.Fatalf("Error connecting %s\n", err)
	}
	c.Watch()
	_, err = c.Wait("rds", 10*time.Second)
	if err != nil {
		log.Fatalf("Error getting initial config %s\n", err)
	}

	if len(c.EDS) == 0 {
		log.Fatal("No endpoints")
	}
	return c
}

func runMcpServer() (*mcptest.Server, error) {
	supportedTypes := make([]string, len(model.IstioConfigTypes))
	for i, m := range model.IstioConfigTypes {
		supportedTypes[i] = fmt.Sprintf("type.googleapis.com/%s", m.MessageName)
	}
	server, err := mcptest.NewServer(8898, supportedTypes)
	if err != nil {
		return nil, err
	}

	return server, nil
}

func initLocalPilotTestEnv(t *testing.T, mcpPort, grpcPort, debugPort int) util.TearDownFunc {
	mixerEnv.NewTestSetup(mixerEnv.PilotMCPTest, t)
	debugAddr := fmt.Sprintf("127.0.0.1:%d", debugPort)
	grpcAddr := fmt.Sprintf("127.0.0.1:%d", grpcPort)
	_, tearDown := util.EnsureTestServer(addMcpAddrs(mcpPort), setupPilotDiscoveryHTTPAddr(debugAddr), setupPilotDiscoveryGrpcAddr(grpcAddr))
	return tearDown
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

func generateServiceEntries() (envelopes []*mcp.Envelope) {
	servicePorts := generatePorts(*serviceEntryCount)
	backendPorts := generatePorts(*serviceEntryCount)
	createTime := createTime()

	for i := 0; i < *serviceEntryCount; i++ {
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

func generateGateway() []*mcp.Envelope {
	return []*mcp.Envelope{
		{
			Metadata: &mcp.Metadata{
				Name:       fmt.Sprintf("gateway-%s", randName()),
				CreateTime: createTime(),
			},
			Resource: &types.Any{
				TypeUrl: "type.googleapis.com/istio.networking.v1alpha3.Gateway",
				Value:   marshaledGateway(),
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

func generatePorts(n int) (ports []int) {
	rand.Seed(time.Now().Unix())
	lowRange := 1023
	highRange := 65535
	for i := 0; i < n; i++ {
		ports = append(ports, rand.Intn(highRange-lowRange)+lowRange)
	}
	return ports
}
