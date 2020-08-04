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

package kube

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwarder manages the forwarding of a single port.
type PortForwarder interface {
	// Run this forwarder.
	Start() error

	// Address returns the local forwarded address. Only valid while the forwarder is running.
	Address() string

	// Close this forwarder and release an resources.
	Close()

	// Block until connection closed (e.g. control-C interrupt)
	WaitForStop()
}

var _ PortForwarder = &forwarder{}

type forwarder struct {
	forwarder *portforward.PortForwarder
	stopCh    chan struct{}
	readyCh   <-chan struct{}
	address   string
	output    *bytes.Buffer
}

func (f *forwarder) Start() error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- f.forwarder.ForwardPorts()
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("failure running port forward process: %v", err)
	case <-f.readyCh:
		// The forwarder is now ready.
		return nil
	}
}

func (f *forwarder) Address() string {
	return f.address
}

func (f *forwarder) Close() {
	close(f.stopCh)
	// Closing the stop channel should close anything
	// opened by f.forwarder.ForwardPorts()
}

func (f *forwarder) WaitForStop() {
	<-f.stopCh
}

func newPortForwarder(restConfig *rest.Config, podName, ns, localAddress string, localPort, podPort int) (PortForwarder, error) {
	restClient, err := rest.RESTClientFor(restConfig)
	if err != nil {
		return nil, err
	}

	req := restClient.Post().Resource("pods").Namespace(ns).Name(podName).SubResource("portforward")
	serverURL := req.URL()

	roundTripper, upgrader, err := roundTripperFor(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failure creating roundtripper: %v", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, serverURL)

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	output := new(bytes.Buffer)
	if localAddress == "" {
		localAddress = defaultLocalAddress
	}
	if localPort == 0 {
		localPort, err = availablePort(localAddress)
		if err != nil {
			return nil, fmt.Errorf("failure allocating port: %v", err)
		}
	}
	fw, err := portforward.NewOnAddresses(dialer,
		[]string{localAddress},
		[]string{fmt.Sprintf("%d:%d", localPort, podPort)},
		stopCh,
		readyCh,
		output,
		os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed establishing port-forward: %v", err)
	}

	// Run the same check as k8s.io/kubectl/pkg/cmd/portforward/portforward.go
	// so that we will fail early if there is a problem contacting API server.
	podGet := restClient.Get().Resource("pods").Namespace(ns).Name(podName)
	obj, err := podGet.Do(context.TODO()).Get()
	if err != nil {
		return nil, fmt.Errorf("failed retrieving pod: %v", err)
	}
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return nil, fmt.Errorf("failed getting pod: %v", err)
	}
	if pod.Status.Phase != v1.PodRunning {
		return nil, fmt.Errorf("pod is not running. Status=%v", pod.Status.Phase)
	}

	return &forwarder{
		forwarder: fw,
		stopCh:    stopCh,
		readyCh:   readyCh,
		output:    output,
		address:   fmt.Sprintf("%s:%d", defaultLocalAddress, localPort),
	}, nil
}

func availablePort(localAddr string) (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", localAddr+":0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	return port, l.Close()
}
