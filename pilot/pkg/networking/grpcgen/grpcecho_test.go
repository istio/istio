// Copyright Istio Authors
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
//
package grpcgen_test

import (
	"context"
	"fmt"
	"math"
	"net"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	//  To install the xds resolvers and balancers.
	_ "google.golang.org/grpc/xds"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/echo/server/endpoint"
	"istio.io/istio/pkg/test/util/retry"
)

type echoCfg struct {
	version   string
	namespace string
	tls       bool
}

type configGenTest struct {
	*testing.T
	endpoints []endpoint.Instance
	ds        *xds.FakeDiscoveryServer
	xdsPort   int
}

// newConfigGenTest creates a FakeDiscoveryServer that listens for gRPC on grpcXdsAddr
// For each of the given servers, we serve echo (only supporting Echo, no ForwardEcho) and
// create a corresponding WorkloadEntry. The WorkloadEntry will have the given format:
//
//    meta:
//      name: echo-{generated portnum}-{server.version}
//      namespace: {server.namespace or "default"}
//      labels: {"app": "grpc", "version": "{server.version}"}
//    spec:
//      address: {grpcEchoHost}
//      ports:
//        grpc: {generated portnum}
func newConfigGenTest(t *testing.T, discoveryOpts xds.FakeOptions, servers ...echoCfg) *configGenTest {
	if runtime.GOOS == "darwin" && len(servers) > 1 {
		// TODO always skip if this breaks anywhere else
		t.Skip("cannot use 127.0.0.2-255 on OSX without manual setup")
	}

	cgt := &configGenTest{T: t}
	wg := sync.WaitGroup{}
	cfgs := []config.Config{}

	discoveryOpts.ListenerBuilder = func() (net.Listener, error) {
		return net.Listen("tcp", "127.0.0.1:0")
	}
	// Start XDS server
	cgt.ds = xds.NewFakeDiscoveryServer(t, discoveryOpts)
	_, xdsPorts, _ := net.SplitHostPort(cgt.ds.Listener.Addr().String())
	xdsPort, _ := strconv.Atoi(xdsPorts)
	cgt.xdsPort = xdsPort
	for i, s := range servers {
		if s.namespace == "" {
			s.namespace = "default"
		}
		// TODO this breaks without extra ifonfig aliases on OSX, and probably elsewhere
		ip := fmt.Sprintf("127.0.0.%d", i+1)

		ep, err := endpoint.New(endpoint.Config{
			Port: &common.Port{
				Name:             "grpc",
				Port:             0,
				Protocol:         protocol.GRPC,
				XDSServer:        true,
				XDSReadinessTLS:  s.tls,
				XDSTestBootstrap: GRPCBootstrap("echo-"+s.version, s.namespace, ip, xdsPort),
			},
			ListenerIP: ip,
			Version:    s.version,
		})
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		if err := ep.Start(func() {
			wg.Done()
		}); err != nil {
			t.Fatal(err)
		}

		cfgs = append(cfgs, makeWE(s, ip, ep.GetConfig().Port.Port))
		cgt.endpoints = append(cgt.endpoints, ep)
		t.Cleanup(func() {
			if err := ep.Close(); err != nil {
				t.Errorf("failed to close endpoint %s: %v", ip, err)
			}
		})
	}
	for _, cfg := range cfgs {
		if _, err := cgt.ds.Env().Create(cfg); err != nil {
			t.Fatalf("failed to create config %v: %v", cfg.Name, err)
		}
	}
	// we know onReady will get called because there are internal timeouts for this
	wg.Wait()
	return cgt
}

func makeWE(s echoCfg, host string, port int) config.Config {
	ns := "default"
	if s.namespace != "" {
		ns = s.namespace
	}
	return config.Config{
		Meta: config.Meta{
			Name:             fmt.Sprintf("echo-%d-%s", port, s.version),
			Namespace:        ns,
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Workloadentries.Resource().GroupVersionKind(),
			Labels: map[string]string{
				"app":     "echo",
				"version": s.version,
			},
		},
		Spec: &networking.WorkloadEntry{
			Address: host,
			Ports:   map[string]uint32{"grpc": uint32(port)},
		},
	}
}

func (t *configGenTest) dialEcho(addr string) *echo.Client {
	resolver := resolverForTest(t, t.xdsPort, "default")
	out, err := echo.New(addr, nil, grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTrafficShifting(t *testing.T) {
	tt := newConfigGenTest(t, xds.FakeOptions{
		KubernetesObjectString: `
apiVersion: v1
kind: Service
metadata:
  labels:
    app: echo-app
  name: echo-app
  namespace: default
spec:
  clusterIP: 1.2.3.4
  selector:
    app: echo
  ports:
  - name: grpc
    targetPort: grpc
    port: 7070
`,
		ConfigString: `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: echo-dr
  namespace: default
spec:
  host: echo-app.default.svc.cluster.local
  subsets:
    - name: v1
      labels:
        version: v1
    - name: v2
      labels:
        version: v2
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: echo-vs
  namespace: default
spec:
  hosts:
  - echo-app.default.svc.cluster.local
  http:
  - route:
    - destination:
        host: echo-app.default.svc.cluster.local
        subset: v1
      weight: 20
    - destination:
        host: echo-app.default.svc.cluster.local
        subset: v2
      weight: 80

`,
	}, echoCfg{version: "v1"}, echoCfg{version: "v2"})

	retry.UntilSuccessOrFail(tt.T, func() error {
		cw := tt.dialEcho("xds:///echo-app.default.svc.cluster.local:7070")
		distribution := map[string]int{}
		for i := 0; i < 100; i++ {
			res, err := cw.Echo(context.Background(), &proto.EchoRequest{Message: "needle"})
			if err != nil {
				return err
			}
			distribution[res.Version]++
		}

		if err := expectAlmost(distribution["v1"], 20); err != nil {
			return err
		}
		if err := expectAlmost(distribution["v2"], 80); err != nil {
			return err
		}
		return nil
	}, retry.Timeout(5*time.Second), retry.Delay(0))
}

func TestMtls(t *testing.T) {
	tt := newConfigGenTest(t, xds.FakeOptions{
		KubernetesObjectString: `
apiVersion: v1
kind: Service
metadata:
  labels:
    app: echo-app
  name: echo-app
  namespace: default
spec:
  clusterIP: 1.2.3.4
  selector:
    app: echo
  ports:
  - name: grpc
    targetPort: grpc
    port: 7070
`,
		ConfigString: `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: echo-dr
  namespace: default
spec:
  host: echo-app.default.svc.cluster.local
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
---
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: default
spec:
  mtls:
    mode: STRICT
`,
	}, echoCfg{version: "v1", tls: true})

	// ensure we can make 10 consecutive successful requests
	retry.UntilSuccessOrFail(tt.T, func() error {
		cw := tt.dialEcho("xds:///echo-app.default.svc.cluster.local:7070")
		for i := 0; i < 10; i++ {
			_, err := cw.Echo(context.Background(), &proto.EchoRequest{Message: "needle"})
			if err != nil {
				return err
			}
		}
		return nil
	}, retry.Timeout(5*time.Second), retry.Delay(0))
}

func TestFault(t *testing.T) {
	tt := newConfigGenTest(t, xds.FakeOptions{
		KubernetesObjectString: `
apiVersion: v1
kind: Service
metadata:
  labels:
    app: echo-app
  name: echo-app
  namespace: default
spec:
  clusterIP: 1.2.3.4
  selector:
    app: echo
  ports:
  - name: grpc
    targetPort: grpc
    port: 7071
`,
		ConfigString: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: echo-delay
spec:
  hosts:
  - echo-app.default.svc.cluster.local
  http:
  - fault:
      delay:
        percent: 100
        fixedDelay: 100ms
    route:
    - destination:
        host: echo-app.default.svc.cluster.local
`,
	}, echoCfg{version: "v1"})
	c := tt.dialEcho("xds:///echo-app.default.svc.cluster.local:7071")

	// without a delay it usually takes ~500us
	st := time.Now()
	_, err := c.Echo(context.Background(), &proto.EchoRequest{})
	duration := time.Since(st)
	if err != nil {
		t.Fatal(err)
	}
	if duration < time.Millisecond*100 {
		t.Fatalf("expected to take over 1s but took %v", duration)
	}

	// TODO test timeouts, aborts
}

func expectAlmost(got, want int) error {
	if math.Abs(float64(want-got)) > 10 {
		return fmt.Errorf("expected within %d of %d but got %d", 10, want, got)
	}
	return nil
}
