package cli

import (
	"context"
	"fmt"

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

func (c MockClient) EnvoyDo(ctx context.Context, podName, podNamespace, method, path string) ([]byte, error) {
	results, ok := c.Results[podName]
	if !ok {
		return nil, fmt.Errorf("unable to retrieve Pod: pods %q not found", podName)
	}
	return results, nil
}

func (c MockClient) EnvoyDoWithPort(ctx context.Context, podName, podNamespace, method, path string, port int) ([]byte, error) {
	results, ok := c.Results[podName]
	if !ok {
		return nil, fmt.Errorf("unable to retrieve Pod: pods %q not found", podName)
	}
	return results, nil
}
