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

package zipkin

import (
	"fmt"
	"io"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"os/exec"
	"time"
)

const (
	portNumber      = "9411"
	maxStartupTries = 40
)

var (
	_ components.Zipkin = &nativeComponent{}
	_ api.Component     = &nativeComponent{}
	_ io.Closer         = &nativeComponent{}
)

// NewNativeComponent factory function for the component
func NewNativeComponent() (api.Component, error) {
	return &nativeComponent{}, nil
}

type nativeComponent struct {
	*client

	scope lifecycle.Scope
	cmd   *exec.Cmd
}

// Descriptor implements Component.Descriptor.
func (c *nativeComponent) Descriptor() component.Descriptor {
	return descriptors.Zipkin
}

// Scope implements Component.Scope.
func (c *nativeComponent) Scope() lifecycle.Scope {
	return c.scope
}

// Start implements Component.Start.
func (c *nativeComponent) Start(ctx context.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	// Map the port for Docker and run the zipkin image.
	portMap := fmt.Sprint(portNumber, ":", portNumber)

	// TODO(sven): This is just the head image, we should specify a version.
	c.cmd = exec.Command("docker", "run", "-p", portMap, "openzipkin/zipkin")

	// Start the command, then wait until the port is ready.
	err = c.cmd.Start()
	if err != nil {
		fmt.Println(err)
	}

	c.client = &client{
		address: fmt.Sprint("localhost:", portNumber),
	}

	// Try up to maxStartupTries to list services from zipkin. Zipkin takes a while to startup.
	for i := 0; i < maxStartupTries; i++ {
		_, listErr := c.ListServices()
		if listErr == nil {
			return nil
		}
		// Failed to list the services, sleep for 1/2 second.
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("Failed to connect to zipkin.")
}

// Close implements io.Closer.
func (c *nativeComponent) Close() (err error) {
	if c.cmd != nil {
		err = c.cmd.Process.Kill()
		if err != nil {
			fmt.Println(err)
			return
		}
		c.cmd.Process.Wait()
		if err != nil {
			fmt.Println(err)
			return
		}
	}

	return
}
