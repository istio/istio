// Copyright 2018 Istio Authors
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
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

var (
	outputBufferSize = 4096
	// Used to extract the first match from the regexes below. If the indexes of the two regexes ever diverge
	// we'l need an index per type.
	addressMatchIndex = 1
	forwardRegexIPv4  = regexp.MustCompile(`Forwarding from (127.0.0.1:[0-9]+) -> ([0-9]+)`)
	forwardRegexIPv6  = regexp.MustCompile(`Forwarding from (\[::1]:[0-9]+) -> ([0-9]+)`)
)

// PortForwarder manages the forwarding of a single port.
type PortForwarder interface {
	io.Closer
	// Start the forwarder.
	Start() error
	// Address returns the local forwarded address. Only valid while the forwarder is running.
	Address() string
}

type defaultPortForwarder struct {
	forwarder *portforward.PortForwarder
	stopCh    chan struct{}
	readyCh   <-chan struct{}
	address   string
	output    *bytes.Buffer
}

func (f *defaultPortForwarder) Start() error {
	errCh := make(chan error)
	go func() {
		errCh <- f.forwarder.ForwardPorts()
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("failure running port forward process: %v", err)
	case <-f.readyCh:
		address, err := parseAddress(f.output.String())
		if err != nil {
			return err
		}
		f.address = address
		return nil
	}
}

func (f *defaultPortForwarder) Address() string {
	return f.address
}

func (f *defaultPortForwarder) Close() error {
	close(f.stopCh)
	f.forwarder.Close()
	return nil
}

func newPortForwarder(restConfig *rest.Config, pod v1.Pod, localPort, remotePort uint16) (PortForwarder, error) {
	restClient, err := rest.RESTClientFor(restConfig)
	if err != nil {
		return nil, err
	}

	req := restClient.Post().Resource("pods").Namespace(pod.Namespace).Name(pod.Name).SubResource("portforward")
	serverURL := req.URL()

	roundTripper, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failure creating roundtripper: %v", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, serverURL)

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	output := bytes.NewBuffer(make([]byte, 0, outputBufferSize))
	fw, err := portforward.New(dialer, []string{fmt.Sprintf("%d:%d", localPort, remotePort)}, stopCh, readyCh, output, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed establishing port-forward: %v", err)
	}

	return &defaultPortForwarder{
		forwarder: fw,
		stopCh:    stopCh,
		readyCh:   readyCh,
		output:    output,
	}, nil
}

func parseAddress(output string) (string, error) {
	// TODO: improve this when we have multi port inputs.
	if matches := forwardRegexIPv4.FindStringSubmatch(output); matches != nil {
		return matches[addressMatchIndex], nil
	} else if matches = forwardRegexIPv6.FindStringSubmatch(output); matches != nil {
		return matches[addressMatchIndex], nil
	}
	return "", fmt.Errorf("failed to get address from output: %s", output)
}
