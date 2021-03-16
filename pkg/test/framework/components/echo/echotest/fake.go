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

package echotest

import (
	"fmt"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

var _ echo.Instance = fakeInstance{}

func instanceKey(i echo.Instance) string {
	return fmt.Sprintf("%s.%s.%s", i.Config().Service, i.Config().Namespace.Name(), i.Config().Cluster.Name())
}

// fakeInstance wraps echo.Config for test-framework internals tests where we don't actually make calls
type fakeInstance echo.Config

func (f fakeInstance) ID() resource.ID {
	panic("implement me")
}

func (f fakeInstance) Config() echo.Config {
	cfg := echo.Config(f)
	_ = common.FillInDefaults(nil, &cfg)
	return cfg
}

func (f fakeInstance) Address() string {
	panic("implement me")
}

func (f fakeInstance) Workloads() ([]echo.Workload, error) {
	panic("implement me")
}

func (f fakeInstance) WorkloadsOrFail(t test.Failer) []echo.Workload {
	panic("implement me")
}

func (f fakeInstance) Call(options echo.CallOptions) (client.ParsedResponses, error) {
	panic("implement me")
}

func (f fakeInstance) CallOrFail(t test.Failer, options echo.CallOptions) client.ParsedResponses {
	panic("implement me")
}

func (f fakeInstance) CallWithRetry(options echo.CallOptions, retryOptions ...retry.Option) (client.ParsedResponses, error) {
	panic("implement me")
}

func (f fakeInstance) CallWithRetryOrFail(t test.Failer, options echo.CallOptions, retryOptions ...retry.Option) client.ParsedResponses {
	panic("implement me")
}

func (f fakeInstance) Restart() error {
	panic("implement me")
}

var _ namespace.Instance = fakeNamespace("")

// fakeNamespace allows matching echo.Configs against a namespace.Instance
type fakeNamespace string

func (f fakeNamespace) Name() string {
	return string(f)
}

func (f fakeNamespace) SetLabel(key, value string) error {
	panic("cannot interact with fake namespace, should not be exposed outside of staticvm")
}

func (f fakeNamespace) RemoveLabel(key string) error {
	panic("cannot interact with fake namespace, should not be exposed outside of staticvm")
}
