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

package agent

import (
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/pkg/model"
)

// Agent controls the startup of a local application and an Envoy proxy, thereby integrating the application into a local Istio service mesh.
type Agent struct {
	// ServiceName the name for the service represented by this Agent.
	ServiceName string

	// App is the Application for which a Service will be created. The lifecycle (start/stop) of the Application will be controlled by the Agent.
	AppFactory ApplicationFactory

	// ProxyFactory is function for creating ApplicationProxy instances.
	ProxyFactory ApplicationProxyFactory

	// ConfigStore will receive a service entry configuration when the agent is started.
	ConfigStore model.ConfigStore

	app         Application
	appStopFunc StopFunc

	proxy         ApplicationProxy
	proxyStopFunc StopFunc
}

// Start starts Envoy and the application.
func (a *Agent) Start() (err error) {
	// Create and start the backend application
	a.app, a.appStopFunc, err = a.AppFactory()
	if err != nil {
		return err
	}

	// Create and start the proxy for the application.
	a.proxy, a.proxyStopFunc, err = a.ProxyFactory(a.ServiceName, a.app)
	if err != nil {
		return err
	}

	// Now add a service entry for this agent.
	_, err = a.ConfigStore.Create(a.proxy.GetConfig())
	if err != nil {
		return err
	}
	return nil
}

// Stop stops Envoy and the application.
func (a *Agent) Stop() (err error) {
	if a.proxyStopFunc != nil {
		err = multierror.Append(a.proxyStopFunc())
	}
	return multierror.Append(err, a.appStopFunc())
}

// GetProxy returns the proxy for the backend Application. Only valid after the Agent has been started.
func (a *Agent) GetProxy() ApplicationProxy {
	return a.proxy
}
