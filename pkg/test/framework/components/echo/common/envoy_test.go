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

package common_test

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"testing"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/golang/protobuf/jsonpb"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/structpath"
)

func TestCheckOutboundConfig(t *testing.T) {
	configDump, err := ioutil.ReadFile("testdata/config_dump.json")
	if err != nil {
		t.Fatal(err)
	}

	cfg := &envoyAdmin.ConfigDump{}
	if err := jsonpb.Unmarshal(bytes.NewReader(configDump), cfg); err != nil {
		t.Fatal(err)
	}

	cluster := resource.FakeCluster{
		NameValue: "cluster-0",
	}

	src := testConfig{
		cluster: cluster,
	}

	cfgs := []testConfig{
		{
			protocol:    protocol.HTTP,
			service:     "b",
			namespace:   "apps-1-99281",
			domain:      "cluster.local",
			servicePort: 80,
			address:     "10.43.241.185",
			cluster:     cluster,
		},
		{
			protocol:    protocol.HTTP,
			service:     "b",
			namespace:   "apps-1-99281",
			domain:      "cluster.local",
			servicePort: 8080,
			address:     "10.43.241.185",
			cluster:     cluster,
		},
		{
			protocol:    protocol.TCP,
			service:     "b",
			namespace:   "apps-1-99281",
			domain:      "cluster.local",
			servicePort: 90,
			address:     "10.43.241.185",
			cluster:     cluster,
		},
		{
			protocol:    protocol.HTTPS,
			service:     "b",
			namespace:   "apps-1-99281",
			domain:      "cluster.local",
			servicePort: 9090,
			address:     "10.43.241.185",
			cluster:     cluster,
		},
		{
			protocol:    protocol.HTTP2,
			service:     "b",
			namespace:   "apps-1-99281",
			domain:      "cluster.local",
			servicePort: 70,
			address:     "10.43.241.185",
			cluster:     cluster,
		},
		{
			protocol:    protocol.GRPC,
			service:     "b",
			namespace:   "apps-1-99281",
			domain:      "cluster.local",
			servicePort: 7070,
			address:     "10.43.241.185",
			cluster:     cluster,
		},
	}

	validator := structpath.ForProto(cfg)

	for _, cfg := range cfgs {
		t.Run(fmt.Sprintf("%s_%d[%s]", cfg.service, cfg.servicePort, cfg.protocol), func(t *testing.T) {
			if err := common.CheckOutboundConfig(&src, &cfg, cfg.Config().Ports[0], validator); err != nil {
				t.Fatal(err)
			}
		})
	}
}

var _ echo.Instance = &testConfig{}
var _ echo.Workload = &testConfig{}

type testConfig struct {
	protocol    protocol.Instance
	servicePort int
	address     string
	service     string
	domain      string
	namespace   string
	cluster     resource.Cluster
}

func (e *testConfig) Owner() echo.Instance {
	return e
}

func (e *testConfig) Port() echo.Port {
	return echo.Port{
		ServicePort: e.servicePort,
		Protocol:    e.protocol,
	}
}

func (e *testConfig) Address() string {
	return e.address
}

func (e *testConfig) Config() echo.Config {
	return echo.Config{
		Cluster: e.cluster,
		Service: e.service,
		Namespace: &fakeNamespace{
			name: e.namespace,
		},
		Domain: e.domain,
		Ports: []echo.Port{
			{
				ServicePort: e.servicePort,
				Protocol:    e.protocol,
			},
		},
	}
}

func (e *testConfig) Workloads() ([]echo.Workload, error) {
	return []echo.Workload{e}, nil
}

func (*testConfig) ID() resource.ID {
	panic("not implemented")
}

func (*testConfig) WorkloadsOrFail(t test.Failer) []echo.Workload {
	panic("not implemented")
}

func (*testConfig) WaitUntilCallable(_ ...echo.Instance) error {
	panic("not implemented")
}

func (*testConfig) WaitUntilCallableOrFail(_ test.Failer, _ ...echo.Instance) {
	panic("not implemented")
}

func (*testConfig) Call(_ echo.CallOptions) (client.ParsedResponses, error) {
	panic("not implemented")
}

func (*testConfig) CallOrFail(_ test.Failer, _ echo.CallOptions) client.ParsedResponses {
	panic("not implemented")
}

func (*testConfig) Sidecar() echo.Sidecar {
	panic("not implemented")
}

func (*testConfig) ForwardEcho(context.Context, *proto.ForwardEchoRequest) (client.ParsedResponses, error) {
	panic("not implemented")
}

type fakeNamespace struct {
	name string
}

func (n *fakeNamespace) Name() string {
	return n.name
}

func (n *fakeNamespace) ID() resource.ID {
	panic("not implemented")
}

func (*testConfig) Logs() (string, error) {
	panic("not implemented")
}

func (*testConfig) LogsOrFail(_ test.Failer) string {
	panic("not implemented")
}
