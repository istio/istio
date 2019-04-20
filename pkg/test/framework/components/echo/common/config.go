// Copyright 2019 Istio Authors
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

	"istio.io/istio/pilot/pkg/model"
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

	// If no namespace was provided, use the default.
	if c.Namespace == nil {
		if c.Namespace, err = namespace.New(ctx, defaultNamespace, true); err != nil {
			return err
		}
	}

	// Append a gRPC port, if none was provided. This is needed
	// for controlling the app.
	if GetGRPCPort(c) == nil {
		c.Ports = append([]echo.Port{
			{
				Name:     "grpc",
				Protocol: model.ProtocolGRPC,
			},
		}, c.Ports...)
	}

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

	return nil
}

func GetGRPCPort(c *echo.Config) *echo.Port {
	for _, p := range c.Ports {
		if p.Protocol == model.ProtocolGRPC {
			return &p
		}
	}
	return nil
}
