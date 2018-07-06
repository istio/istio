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

package echo

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/local/envoy/agent"
	"istio.io/istio/pkg/test/protocol"
)

// Factory is a factory for echo applications.
type Factory struct {
	Ports   model.PortList
	TLSCert string
	TLSCKey string
	Version string
}

// NewApplication is an agent.ApplicationFactory function that manufactures echo applications.
func (f *Factory) NewApplication(client protocol.Client) (agent.Application, error) {

	// Make a copy of the port list.
	ports := make(model.PortList, len(f.Ports))
	for i, p := range f.Ports {
		tempP := *p
		ports[i] = &tempP
	}

	app := &Application{
		Ports:   ports,
		TLSCert: f.TLSCert,
		TLSCKey: f.TLSCKey,
		Version: f.Version,
		Client:  client,
	}
	if err := app.Start(); err != nil {
		return nil, err
	}

	return app, nil
}
