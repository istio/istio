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

package cli

import (
	"context"
	"fmt"

	"google.golang.org/grpc/credentials"

	"istio.io/istio/pkg/kube"
)

type MockPortForwarder struct{}

func (m MockPortForwarder) Start() error {
	return nil
}

func (m MockPortForwarder) Address() string {
	return "localhost:3456"
}

func (m MockPortForwarder) Close() {
}

func (m MockPortForwarder) ErrChan() <-chan error {
	return make(chan error)
}

func (m MockPortForwarder) WaitForStop() {
}

var _ kube.PortForwarder = MockPortForwarder{}

type MockClient struct {
	// Results is a map of podName to the results of the expected test on the pod
	Results map[string][]byte
	kube.CLIClient
}

func (c MockClient) NewPortForwarder(_, _, _ string, _, _ int) (kube.PortForwarder, error) {
	return MockPortForwarder{}, nil
}

func (c MockClient) AllDiscoveryDo(_ context.Context, _, _ string) (map[string][]byte, error) {
	return c.Results, nil
}

func (c MockClient) EnvoyDoWithPort(ctx context.Context, podName, podNamespace, method, path string, port int) ([]byte, error) {
	results, ok := c.Results[podName]
	if !ok {
		return nil, fmt.Errorf("unable to retrieve Pod: pods %q not found", podName)
	}
	return results, nil
}

func (c MockClient) CreatePerRPCCredentials(_ context.Context, _, _ string, _ []string, _ int64,
) (credentials.PerRPCCredentials, error) {
	return nil, nil
}
