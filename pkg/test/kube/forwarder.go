//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// TODO: Consider replacing this code with a direct call to the underlying client library, or wrapping
// the executable call in a shell function, so that the process doesn't end up running indefinitely in case
// of abrupt end to the test run.
// See https://github.com/istio/istio/issues/6173

// PortForwarder represents a locally started kubectl process that forwards a local port to one within the
// cluster.
type PortForwarder struct {
	ctx     context.Context
	cancel  context.CancelFunc
	cmd     *exec.Cmd
	address string
	target  string
}

// NewPortForwarder returns a new PortForwarder the specified port in the given namespace/pod.
func NewPortForwarder(kubeconfig string, ns string, pod string, port int) *PortForwarder {
	command := fmt.Sprintf("kubectl port-forward --kubeconfig=%s -n %s %s :%d", kubeconfig, ns, pod, port)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sh", "-c", command)

	return &PortForwarder{
		ctx:    ctx,
		cancel: cancel,
		cmd:    cmd,
		target: fmt.Sprintf("%s/%s (%d)", ns, pod, port),
	}
}

// Address is the local address for the port forwarder.
func (p *PortForwarder) Address() string {
	return p.address
}

// Start the port forwarder.
func (p *PortForwarder) Start() error {
	pipe, err := p.cmd.StdoutPipe()
	if err != nil {
		panic("")
	}
	if err != nil {
		return err
	}

	err = p.cmd.Start()
	if err != nil {
		return err
	}

	// Parse the first line from kubectl to scrape the local address.
	buffer := make([]byte, 4096)
	pos := 0
	_, err = retry(defaultTimeout, defaultRetryWait, func() (interface{}, bool, error) {

		ptr := buffer[pos:]
		n, err2 := pipe.Read(ptr)
		if err2 != nil {
			_ = p.cmd.Process.Kill()
			return nil, true, err2
		}
		pos += n
		s := string(buffer[0:pos])
		if strings.Contains(s, "\n") {
			s = s[len("Forwarding from "):]
			parts := strings.Split(s, "->")
			p.address = strings.TrimSpace(parts[0])
			scope.Debugf("Starting port forward: %s => %s", p.address, p.target)
			return nil, true, nil
		}

		return nil, false, nil
	})

	return err
}

// Close the port forwarder.
func (p *PortForwarder) Close() {
	p.cancel()
	if p.cmd != nil {
		_ = p.cmd.Wait()
	}
}
