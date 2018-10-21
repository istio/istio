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

package local

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/envoy"
	"istio.io/istio/pkg/test/framework/components/apps/api"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/local"
	"istio.io/istio/pkg/test/framework/environments/local/service"
)

const (
	timeout       = 10 * time.Second
	retryInterval = 500 * time.Millisecond
)

var (
	// Component provides a framework component for the local environment.
	Component = &localComponent{}
)

type localComponent struct{}

// ID implements the component.Component interface.
func (c *localComponent) ID() dependency.Instance {
	return dependency.Apps
}

// Requires implements the component.Component interface.
func (c *localComponent) Requires() []dependency.Instance {
	return []dependency.Instance{
		dependency.Pilot,
	}
}

// Init implements the component.Component interface.
func (c *localComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (interface{}, error) {
	e, ok := ctx.Environment().(*local.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	p, ok := deps[dependency.Pilot].(pilot.LocalPilot)
	if !ok {
		return nil, fmt.Errorf("missing dependency: %s", dependency.Pilot)
	}

	return NewApps(p.GetDiscoveryAddress(), e.ServiceManager)
}

type appsImpl struct {
	tlsCKey          string
	tlsCert          string
	discoveryAddress *net.TCPAddr
	serviceManager   *service.Manager
	apps             []environment.DeployedApp
}

// NewApps creates a new apps bundle. Visible for testing.
func NewApps(discoveryAdddress *net.TCPAddr, serviceManager *service.Manager) (out api.Apps, err error) {
	cfgs := []appConfig{
		{
			serviceName: "a",
			version:     "v1",
		},
		{
			serviceName: "b",
			version:     "unversioned",
		},
		{
			serviceName: "c",
			version:     "v1",
		},
		// TODO(nmittler): Investigate how to support multiple versions in the local environment.
		/*{
			serviceName: "c",
			version:     "v2",
		},*/
	}

	impl := &appsImpl{
		// TODO(nmittler): tlsCKey:
		// TODO(nmittler): tlsCert:
		discoveryAddress: discoveryAdddress,
		serviceManager:   serviceManager,
	}
	impl.apps = make([]environment.DeployedApp, len(cfgs))
	defer func() {
		if err != nil {
			impl.Close()
		}
	}()

	for i, cfg := range cfgs {
		cfg.tlsCKey = impl.tlsCert
		cfg.tlsCert = impl.tlsCert
		cfg.discoveryAddress = impl.discoveryAddress
		cfg.serviceManager = impl.serviceManager

		impl.apps[i], err = newApp(cfg)
		if err != nil {
			return nil, err
		}
	}

	if err = impl.waitForAppConfigDistribution(); err != nil {
		return nil, err
	}

	return impl, nil
}

// GetApp implements the apps.Apps interface.
func (m *appsImpl) GetApp(name string) (environment.DeployedApp, error) {
	for _, a := range m.apps {
		if a.Name() == name {
			return a, nil
		}
	}
	return nil, fmt.Errorf("app %s does not exist", name)
}

func (m *appsImpl) Close() (err error) {
	for i, a := range m.apps {
		if a != nil {
			err = multierror.Append(err, a.(*app).Close()).ErrorOrNil()
			m.apps[i] = nil
		}
	}
	return
}

func (m *appsImpl) waitForAppConfigDistribution() error {
	// Wait for config for all services to be distributed to all Envoys.
	endTime := time.Now().Add(timeout)
	for _, src := range m.apps {
		for _, target := range m.apps {
			if src == target {
				continue
			}
			for {
				err := src.(*app).agent.CheckConfiguredForService(target.(*app).agent)
				if err == nil {
					break
				}

				if time.Now().After(endTime) {
					out := fmt.Sprintf("failed to configure apps: %v. Dumping Envoy configurations:\n", err)
					for _, a := range m.apps {
						dump, _ := configDumpStr(a)
						out += fmt.Sprintf("app %s Config: %s\n", a.Name(), dump)
					}

					return errors.New(out)
				}
				time.Sleep(retryInterval)
			}
		}
	}
	return nil
}

func configDumpStr(a environment.DeployedApp) (string, error) {
	return envoy.GetConfigDumpStr(a.(*app).agent.GetAdminPort())
}
