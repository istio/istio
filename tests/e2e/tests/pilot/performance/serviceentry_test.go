// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package performance

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/google/uuid"
	"github.com/onsi/gomega"

	mcp "istio.io/api/mcp/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	mixerEnv "istio.io/istio/mixer/test/client/env"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/mcp/snapshot"
	"istio.io/istio/pkg/mcp/source"
	mcptest "istio.io/istio/pkg/mcp/testing"
	"istio.io/istio/pkg/mcp/testing/groups"
	"istio.io/istio/tests/util"
)

var (
	serviceEntryCount = flag.Int("serviceEntryCount", 1000, "Number of Service Entries to register")
	terminateAfter    = flag.Duration("terminateAfter", 60*time.Second, "Number of seconds to run the test")
	envoyCount        = flag.Int("adsc", 1, "Number of concurrent envoys to connect to pilot")
)

const (
	pilotDebugPort = 5555
	pilotGrpcPort  = 15010
	serviceDomain  = "example.com"
)

func TestServiceEntry(t *testing.T) {
	t.Logf("running performance test with the following parameters { Number of ServiceEntry: %d, Number of Envoys: %d, Total test time: %vs }",
		*serviceEntryCount,
		*envoyCount,
		terminateAfter.Seconds())

	t.Log("building & starting mock mcp server...")
	mcpServer, err := runMcpServer()
	if err != nil {
		t.Fatal(err)
	}
	defer mcpServer.Close()

	quit := make(chan struct{})
	go runSnapshot(mcpServer, quit, t)

	debugAddr := fmt.Sprintf("127.0.0.1:%d", pilotDebugPort)
	pilotAddr := fmt.Sprintf("127.0.0.1:%d", pilotGrpcPort)

	tearDown := initLocalPilotTestEnv(t, mcpServer.Port, pilotAddr, debugAddr)
	defer tearDown()

	t.Log("run sidecar envoy(s)...")
	adscs := adsConnectAndWait(*envoyCount, pilotAddr, t)
	for _, adsc := range adscs {
		defer adsc.Close()
	}

	t.Log("check for service entries in pilot's debug endpoint...")
	g := gomega.NewGomegaWithT(t)
	g.Eventually(func() (int, error) {
		return registeredServiceEntries(fmt.Sprintf("http://127.0.0.1:%d/debug/configz", pilotDebugPort))
	}, "30s", "1s").Should(gomega.Equal(*serviceEntryCount))

	t.Log("check for registered endpoints in envoys's debug endpoint...")
	g.Eventually(func() error {
		for _, adsc := range adscs {
			var data map[string]interface{}
			err = json.Unmarshal([]byte(adsc.EndpointsJSON()), &data)
			if err != nil {
				return err
			}
			if len(data) != *serviceEntryCount {
				return errors.New("endpoint not registered")
			}
		}
		return nil
	}, "30s", "1s").ShouldNot(gomega.HaveOccurred())

	terminate(quit)
}

func runSnapshot(mcpServer *mcptest.Server, quit chan struct{}, t *testing.T) {
	for {
		// wait for pilot to make a MCP request
		if status := mcpServer.Cache.Status(groups.Default); status != nil {
			if status.Watches() > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	configInterval := time.NewTicker(time.Second * 3)
	v := 0
	b := snapshot.NewInMemoryBuilder()
	for {
		select {
		case <-configInterval.C:
			v++
			version := strconv.Itoa(v)
			for _, m := range model.IstioConfigTypes {
				if m.MessageName == model.ServiceEntry.MessageName {
					b.Set(model.ServiceEntry.Collection, version, generateServiceEntries(t))
				} else if m.MessageName == model.Gateway.MessageName {
					gw, err := generateGateway()
					if err != nil {
						t.Log(err)
					}
					b.Set(model.Gateway.Collection, version, gw)
				} else {
					b.Set(m.Collection, version, []*mcp.Resource{})
				}
			}

			lastSnapshot := time.Now()
			shot := b.Build()
			mcpServer.Cache.SetSnapshot(groups.Default, shot)
			b = shot.Builder()

			// wait for client to ACK the pushed snapshot
			for {
				if status := mcpServer.Cache.Status(groups.Default); status != nil {
					if status.LastWatchRequestTime().After(lastSnapshot) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}

		case <-quit:
			configInterval.Stop()
			return
		}
	}
}

func terminate(quit chan struct{}) {
	<-time.After(*terminateAfter)
	quit <- struct{}{}
}

func adsConnectAndWait(n int, pilotAddr string, t *testing.T) (adscs []*adsc.ADSC) {
	for i := 0; i < n; i++ {
		c, err := adsc.Dial(pilotAddr, "", &adsc.Config{
			IP: net.IPv4(10, 10, byte(i/256), byte(i%256)).String(),
		})
		if err != nil {
			t.Fatal(err)
		}
		c.Watch()
		_, err = c.Wait("eds", 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}

		if len(c.EDS) == 0 {
			t.Fatalf("No endpoints")
		}
		adscs = append(adscs, c)
	}
	return adscs
}

func runMcpServer() (*mcptest.Server, error) {
	collections := make([]string, len(model.IstioConfigTypes))
	for i, m := range model.IstioConfigTypes {
		collections[i] = m.Collection
	}
	return mcptest.NewServer(0, source.CollectionOptionsFromSlice(collections, false))
}

func initLocalPilotTestEnv(t *testing.T, mcpPort int, grpcAddr, debugAddr string) util.TearDownFunc {
	mixerEnv.NewTestSetup(mixerEnv.PilotMCPTest, t)
	_, tearDown := util.EnsureTestServer(addMcpAddrs(mcpPort), setupPilotDiscoveryHTTPAddr(debugAddr), setupPilotDiscoveryGrpcAddr(grpcAddr))
	return tearDown
}

func addMcpAddrs(mcpPort int) func(*bootstrap.PilotArgs) {
	return func(arg *bootstrap.PilotArgs) {
		if arg.MeshConfig == nil {
			arg.MeshConfig = &meshconfig.MeshConfig{}
		}
		arg.MeshConfig.ConfigSources = []*meshconfig.ConfigSource{
			{Address: fmt.Sprintf("127.0.0.1:%d", mcpPort)},
		}
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
	defer resp.Body.Close()

	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	debug := string(respBytes)
	return strings.Count(debug, serviceDomain), nil
}

func generateServiceEntries(t *testing.T) (resources []*mcp.Resource) {
	port := 1

	createTime, err := createTime()
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < *serviceEntryCount; i++ {
		servicePort := port + 1
		backendPort := servicePort + 1
		host := fmt.Sprintf("%s.%s", randName(), serviceDomain)
		body, err := marshaledServiceEntry(servicePort, backendPort, host)
		if err != nil {
			t.Fatal(err)
		}

		resources = append(resources, &mcp.Resource{
			Metadata: &mcp.Metadata{
				Name:       fmt.Sprintf("serviceEntry-%s", randName()),
				CreateTime: createTime,
			},
			Body: body,
		})
		port = backendPort
	}
	return resources
}

func marshaledServiceEntry(servicePort, backendPort int, host string) (*types.Any, error) {
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

	return types.MarshalAny(serviceEntry)
}

func generateGateway() ([]*mcp.Resource, error) {
	gateway := &networking.Gateway{
		Servers: []*networking.Server{
			{
				Port: &networking.Port{
					Name:     "http-8099",
					Number:   8099,
					Protocol: "http",
				},
				Hosts: []string{"*"},
			},
		},
	}

	body, err := types.MarshalAny(gateway)
	if err != nil {
		return nil, err
	}

	createTime, err := createTime()
	if err != nil {
		return nil, err
	}

	return []*mcp.Resource{
		{
			Metadata: &mcp.Metadata{
				Name:       fmt.Sprintf("gateway-%s", randName()),
				CreateTime: createTime,
			},
			Body: body,
		},
	}, nil
}

func createTime() (*types.Timestamp, error) {
	return types.TimestampProto(time.Now())
}

func randName() string {
	name := uuid.New()
	return name.String()
}
