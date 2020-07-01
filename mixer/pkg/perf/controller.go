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

package perf

import (
	"fmt"
	"net"
	"net/http"
	"net/rpc"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/pkg/log"
)

// TODO(lichuqiang): modify defaultTimeout accordingly.
const defaultTimeout = 5 * time.Minute

// Controller is the top-level perf benchmark controller. It drives the test by managing the client(s) that generate
// load against a Mixer instance.
type Controller struct {
	// rpcServer is the RPC listener for the main Controller rpcServer.
	rpcServer *rpc.Server
	// listener is the listener for the RPC rpcServer.
	listener net.Listener

	// rpcPath is the unique HTTP path at which the Controller rpcServer listens on.
	rpcPath string

	// incoming is a channel where incoming clients are published at.
	incoming chan struct{}

	// clients is the current active set of connections to clients.
	clients []*rpc.Client
}

// newController returns a new perf test controller instance.
func newController() (*Controller, error) {
	c := &Controller{
		incoming: make(chan struct{}, 100),
		clients:  []*rpc.Client{},
	}

	// Setup a TCP listener at a random port.
	var err error
	var l net.Listener
	if l, err = net.Listen("tcp", "127.0.0.1:"); err != nil {
		return nil, err
	}
	c.listener = l

	// Generate HTTP paths to listen on
	c.rpcPath = generatePath("controller")
	rpcDebugPath := generateDebugPath("controller")

	c.rpcServer = rpc.NewServer()
	_ = c.rpcServer.Register(c)
	c.rpcServer.HandleHTTP(c.rpcPath, rpcDebugPath)

	go func() {
		// Use locally captured listener, as the listener field on s can change underneath us.
		_ = http.Serve(l, nil)
	}()

	log.Infof("controller is accepting connections on: %s%s", c.listener.Addr().String(), c.rpcPath)
	_ = log.Sync()
	return c, nil
}

func (c *Controller) initializeClients(address string, setup *Setup) error {
	for i, conn := range c.clients {
		var bytes []byte
		var err error
		bytes, err = marshalLoad(&setup.Loads[i])
		if err != nil {
			return err
		}
		params := ClientServerInitParams{Address: address, Load: bytes}
		err = conn.Call("ClientServer.InitializeClient", params, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

// nolinter: unparam
func (c *Controller) runClients(iterations int, timeout time.Duration) error {
	if len(c.clients) == 0 {
		return nil
	}

	if timeout == 0 {
		timeout = defaultTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	resCount := 0
	errCh := make(chan error, len(c.clients))

	for _, conn := range c.clients {
		connc := conn
		// Make calls asynchronously.
		go func() { errCh <- connc.Call("ClientServer.Run", iterations, nil) }()
	}

	var errors *multierror.Error
	for {
		if resCount >= len(c.clients) {
			// All of the calls returned
			break
		}
		select {
		case e := <-errCh:
			resCount++
			if e != nil {
				errors = multierror.Append(errors, e)
			}
		case <-timer.C:
			return fmt.Errorf("timeout waiting for call response")
		}
	}

	return errors.ErrorOrNil()
}

func (c *Controller) close() (err error) {
	log.Infof("Dispatching close to all clients")
	_ = log.Sync()

	for _, conn := range c.clients {
		e := conn.Call("ClientServer.Shutdown", struct{}{}, nil)
		if e != nil && err == nil {
			err = e
		}
		e = conn.Close()
		if e != nil && err == nil {
			err = e
		}
	}
	c.clients = []*rpc.Client{}

	// finally, close our own rpc server.
	if c.listener != nil {
		e := c.listener.Close()
		if e != nil && err == nil {
			err = e
		}

		c.listener = nil
	}

	return err
}

// waitForClient is a convenience method for blocking until the next available client appears.
func (c *Controller) waitForClient() {
	<-c.incoming
}

// location returns the location that the controller rpc server is listening on.
func (c *Controller) location() ServiceLocation {
	return ServiceLocation{Address: c.listener.Addr().String(), Path: c.rpcPath}
}

// RegisterClient is an RPC method called by the clients to registers with this controller.
func (c *Controller) RegisterClient(loc ServiceLocation, _ *struct{}) error {
	log.Infof("Incoming client: %s", loc)
	_ = log.Sync()

	// Connect back to the client's own service.
	conn, err := rpc.DialHTTPPath("tcp", loc.Address, loc.Path)
	if err != nil {
		return err
	}

	log.Infof("Connected to client: %s", loc)
	_ = log.Sync()

	c.clients = append(c.clients, conn)
	c.incoming <- struct{}{}

	return nil
}
