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

package common_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"testing"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"

	"github.com/gogo/protobuf/jsonpb"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/echo/client"
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

	cfgs := []config{
		{
			protocol:    model.ProtocolHTTP,
			service:     "b",
			namespace:   "apps-1-99281",
			domain:      "svc.cluster.local",
			servicePort: 80,
			address:     "10.43.241.185",
		},
		{
			protocol:    model.ProtocolHTTP,
			service:     "b",
			namespace:   "apps-1-99281",
			domain:      "svc.cluster.local",
			servicePort: 8080,
			address:     "10.43.241.185",
		},
		{
			protocol:    model.ProtocolTCP,
			service:     "b",
			namespace:   "apps-1-99281",
			domain:      "svc.cluster.local",
			servicePort: 90,
			address:     "10.43.241.185",
		},
		{
			protocol:    model.ProtocolHTTPS,
			service:     "b",
			namespace:   "apps-1-99281",
			domain:      "svc.cluster.local",
			servicePort: 9090,
			address:     "10.43.241.185",
		},
		{
			protocol:    model.ProtocolHTTP2,
			service:     "b",
			namespace:   "apps-1-99281",
			domain:      "svc.cluster.local",
			servicePort: 70,
			address:     "10.43.241.185",
		},
		{
			protocol:    model.ProtocolGRPC,
			service:     "b",
			namespace:   "apps-1-99281",
			domain:      "svc.cluster.local",
			servicePort: 7070,
			address:     "10.43.241.185",
		},
	}

	validator := structpath.ForProto(cfg)

	for _, cfg := range cfgs {
		t.Run(fmt.Sprintf("%s_%d[%s]", cfg.service, cfg.servicePort, cfg.protocol), func(t *testing.T) {
			if err := common.CheckOutboundConfig(&cfg, cfg.Config().Ports[0], validator); err != nil {
				t.Fatal(err)
			}
		})
	}
}

var _ echo.Instance = &config{}
var _ echo.Workload = &config{}

type config struct {
	protocol    model.Protocol
	servicePort int
	address     string
	service     string
	domain      string
	namespace   string
}

func (e *config) Owner() echo.Instance {
	return e
}

func (e *config) Port() echo.Port {
	return echo.Port{
		ServicePort: e.servicePort,
		Protocol:    e.protocol,
	}
}

func (e *config) Address() string {
	return e.address
}

func (e *config) Config() echo.Config {
	return echo.Config{
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

func (e *config) Workloads() ([]echo.Workload, error) {
	return []echo.Workload{e}, nil
}

func (e *config) ID() resource.ID {
	panic("not implemented")
}

func (e *config) WorkloadsOrFail(t testing.TB) []echo.Workload {
	panic("not implemented")
}

func (e *config) WaitUntilReady(_ ...echo.Instance) error {
	panic("not implemented")
}

func (e *config) WaitUntilReadyOrFail(_ testing.TB, _ ...echo.Instance) {
	panic("not implemented")
}

func (e *config) Call(_ echo.CallOptions) (client.ParsedResponses, error) {
	panic("not implemented")
}

func (e *config) CallOrFail(_ testing.TB, _ echo.CallOptions) client.ParsedResponses {
	panic("not implemented")
}

func (e *config) Sidecar() echo.Sidecar {
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
