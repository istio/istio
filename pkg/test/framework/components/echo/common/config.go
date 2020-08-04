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

package common

import (
	"fmt"
	"strings"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	defaultService   = "echo"
	defaultVersion   = "v1"
	defaultNamespace = "echo"
)

func FillInDefaults(ctx resource.Context, defaultDomain string, c *echo.Config) (err error) {
	if c.Service == "" {
		c.Service = defaultService
	}

	if c.Version == "" {
		c.Version = defaultVersion
	}

	if c.Domain == "" {
		c.Domain = defaultDomain
	}

	// Fill in the default cluster.
	c.Cluster = ctx.Clusters().GetOrDefault(c.Cluster)

	// If no namespace was provided, use the default.
	if c.Namespace == nil {
		nsConfig := namespace.Config{
			Prefix: defaultNamespace,
			Inject: true,
		}
		if c.Namespace, err = namespace.New(ctx, nsConfig); err != nil {
			return err
		}
	}

	// Make a copy of the ports array. This avoids potential corruption if multiple Echo
	// Instances share the same underlying ports array.
	c.Ports = append([]echo.Port{}, c.Ports...)

	// Mark all user-defined ports as used, so the port generator won't assign them.
	portGen := newPortGenerators()
	for _, p := range c.Ports {
		if p.ServicePort > 0 {
			if portGen.Service.IsUsed(p.ServicePort) {
				return fmt.Errorf("failed configuring port %s: service port already used %d", p.Name, p.ServicePort)
			}
			portGen.Service.SetUsed(p.ServicePort)
		}
		if p.InstancePort > 0 {
			if portGen.Instance.IsUsed(p.InstancePort) {
				return fmt.Errorf("failed configuring port %s: instance port already used %d", p.Name, p.InstancePort)
			}
			portGen.Instance.SetUsed(p.InstancePort)
		}
	}
	for _, p := range c.WorkloadOnlyPorts {
		if p.Port > 0 {
			if portGen.Instance.IsUsed(p.Port) {
				return fmt.Errorf("failed configuring workload only port %d: port already used", p.Port)
			}
			portGen.Instance.SetUsed(p.Port)
			if portGen.Service.IsUsed(p.Port) {
				return fmt.Errorf("failed configuring workload only port %d: port already used", p.Port)
			}
			portGen.Service.SetUsed(p.Port)
		}
	}

	// Second pass: try to make unassigned instance ports match service port.
	for i, p := range c.Ports {
		if p.InstancePort <= 0 && p.ServicePort > 0 && !portGen.Instance.IsUsed(p.ServicePort) {
			c.Ports[i].InstancePort = p.ServicePort
			portGen.Instance.SetUsed(p.ServicePort)
		}
	}

	// Final pass: assign default values for any ports that haven't been specified.
	for i, p := range c.Ports {
		if p.ServicePort <= 0 {
			c.Ports[i].ServicePort = portGen.Service.Next(p.Protocol)
		}
		if p.InstancePort <= 0 {
			c.Ports[i].InstancePort = portGen.Instance.Next(p.Protocol)
		}
	}

	// If readiness probe is not specified by a test, wait a long time
	// Waiting forever would cause the test to timeout and lose logs
	if c.ReadinessTimeout == 0 {
		c.ReadinessTimeout = time.Minute * 10
	}

	return nil
}

// GetPortForProtocol returns the first port found with the given protocol, or nil if none was found.
func GetPortForProtocol(c *echo.Config, protocol protocol.Instance) *echo.Port {
	for _, p := range c.Ports {
		if p.Protocol == protocol {
			return &p
		}
	}
	return nil
}

// AddPortIfMissing adds a port for the given protocol if none was found.
func AddPortIfMissing(c *echo.Config, protocol protocol.Instance) {
	if GetPortForProtocol(c, protocol) == nil {
		c.Ports = append([]echo.Port{
			{
				Name:     strings.ToLower(string(protocol)),
				Protocol: protocol,
			},
		}, c.Ports...)
	}
}
