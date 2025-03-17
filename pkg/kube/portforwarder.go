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
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"istio.io/istio/pkg/log"
)

// PortForwarder manages the forwarding of a single port.
type PortForwarder interface {
	// Start runs this forwarder.
	Start() error

	// Address returns the local forwarded address. Only valid while the forwarder is running.
	Address() string

	// Close this forwarder and release an resources.
	Close()

	// ErrChan returns a channel that returns an error when one is encountered. While Start() may return an initial error,
	// the port-forward connection may be lost at anytime. The ErrChan can be read to determine if/when the port-forwarding terminates.
	// This can return nil if the port forwarding stops gracefully.
	ErrChan() <-chan error

	// WaitForStop blocks until connection closed (e.g. control-C interrupt)
	WaitForStop()
}

var _ PortForwarder = &forwarder{}

type forwarder struct {
	stopCh       chan struct{}
	restConfig   *rest.Config
	podName      string
	ns           string
	localAddress string
	localPort    int
	podPort      int
	errCh        chan error
}

func (f *forwarder) Start() error {
	f.errCh = make(chan error, 1)
	readyCh := make(chan struct{}, 1)

	var fw *portforward.PortForwarder
	go func() {
		for {
			select {
			case <-f.stopCh:
				return
			default:
			}
			var err error
			// Build a new port forwarder.
			fw, err = f.buildK8sPortForwarder(readyCh)
			if err != nil {
				f.errCh <- fmt.Errorf("building port forwarded: %v", err)
				return
			}
			if err = fw.ForwardPorts(); err != nil {
				log.Errorf("port forward failed: %v", err)
				f.errCh <- fmt.Errorf("port forward: %v", err)
				return
			}
			log.Debugf("port forward completed without error")
			f.errCh <- nil
			// At this point, either the stopCh has been closed, or port forwarder connection is broken.
			// the port forwarder should have already been ready before.
			// No need to notify the ready channel anymore when forwarding again.
			readyCh = nil
		}
	}()

	// We want to block Start() until we have either gotten an error or have started
	// We may later get an error, but that is handled async.
	select {
	case err := <-f.errCh:
		return fmt.Errorf("failure running port forward process: %v", err)
	case <-readyCh:
		p, err := fw.GetPorts()
		if err != nil {
			return fmt.Errorf("failed to get ports: %v", err)
		}
		if len(p) == 0 {
			return fmt.Errorf("got no ports")
		}
		// Set local port now, as it may have been 0 as input
		f.localPort = int(p[0].Local)
		log.Debugf("Port forward established %v -> %v.%v:%v", f.Address(), f.podName, f.podName, f.podPort)
		// The forwarder is now ready.
		return nil
	}
}

func (f *forwarder) Address() string {
	return net.JoinHostPort(f.localAddress, strconv.Itoa(f.localPort))
}

func (f *forwarder) Close() {
	close(f.stopCh)
	// Closing the stop channel should close anything
	// opened by f.forwarder.ForwardPorts()
}

func (f *forwarder) ErrChan() <-chan error {
	return f.errCh
}

func (f *forwarder) WaitForStop() {
	<-f.stopCh
}

func newPortForwarder(c *client, podName, ns, localAddress string, localPort, podPort int) (PortForwarder, error) {
	if localAddress == "" {
		localAddress = defaultLocalAddress
	}
	f := &forwarder{
		stopCh:       make(chan struct{}),
		restConfig:   c.config,
		podName:      podName,
		ns:           ns,
		localAddress: localAddress,
		localPort:    localPort,
		podPort:      podPort,
	}

	return f, nil
}

func (f *forwarder) buildK8sPortForwarder(readyCh chan struct{}) (*portforward.PortForwarder, error) {
	restClient, err := rest.RESTClientFor(f.restConfig)
	if err != nil {
		return nil, err
	}

	req := restClient.Post().Resource("pods").Namespace(f.ns).Name(f.podName).SubResource("portforward")
	serverURL := req.URL()

	roundTripper, upgrader, err := roundTripperFor(f.restConfig)
	if err != nil {
		return nil, fmt.Errorf("failure creating roundtripper: %v", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, serverURL)
	if !portForwardWebsockets.IsDisabled() {
		tunnelingDialer, err := portforward.NewSPDYOverWebsocketDialer(serverURL, f.restConfig)
		if err != nil {
			return nil, err
		}
		// First attempt tunneling (websocket) dialer, then fallback to spdy dialer.
		dialer = portforward.NewFallbackDialer(tunnelingDialer, dialer, func(err error) bool {
			return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
		})
	}

	fw, err := portforward.NewOnAddresses(dialer,
		[]string{f.localAddress},
		[]string{fmt.Sprintf("%d:%d", f.localPort, f.podPort)},
		f.stopCh,
		readyCh,
		io.Discard,
		os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed establishing port-forward: %v", err)
	}

	// Run the same check as k8s.io/kubectl/pkg/cmd/portforward/portforward.go
	// so that we will fail early if there is a problem contacting API server.
	podGet := restClient.Get().Resource("pods").Namespace(f.ns).Name(f.podName)
	obj, err := podGet.Do(context.TODO()).Get()
	if err != nil {
		return nil, fmt.Errorf("failed retrieving: %v in the %q namespace", err, f.ns)
	}
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return nil, fmt.Errorf("failed getting pod, object type is %T", obj)
	}
	if pod.Status.Phase != v1.PodRunning {
		return nil, fmt.Errorf("pod is not running. Status=%v", pod.Status.Phase)
	}

	return fw, nil
}
