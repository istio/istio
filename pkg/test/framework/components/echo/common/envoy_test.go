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
	"context"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	_ echo.Instance = &testConfig{}
	_ echo.Workload = &testConfig{}
)

type testConfig struct {
	protocol    protocol.Instance
	servicePort int
	address     string
	service     string
	domain      string
	namespace   string
	cluster     cluster.Cluster
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

func (e *testConfig) PodName() string {
	panic("not implemented")
}

func (*testConfig) ID() resource.ID {
	panic("not implemented")
}

func (*testConfig) WorkloadsOrFail(_ test.Failer) []echo.Workload {
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

func (e *testConfig) CallWithRetry(_ echo.CallOptions, _ ...retry.Option) (client.ParsedResponses, error) {
	panic("implement me")
}

func (e *testConfig) CallWithRetryOrFail(_ test.Failer, _ echo.CallOptions, _ ...retry.Option) client.ParsedResponses {
	panic("implement me")
}

func (*testConfig) Sidecar() echo.Sidecar {
	panic("not implemented")
}

func (*testConfig) ForwardEcho(_ context.Context, _ *proto.ForwardEchoRequest) (client.ParsedResponses, error) {
	panic("not implemented")
}

func (*testConfig) Restart() error {
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

func (n *fakeNamespace) SetLabel(key, value string) error {
	panic("not implemented")
}

func (n *fakeNamespace) RemoveLabel(key string) error {
	panic("not implemented")
}

func (*testConfig) Logs() (string, error) {
	panic("not implemented")
}

func (*testConfig) LogsOrFail(_ test.Failer) string {
	panic("not implemented")
}
