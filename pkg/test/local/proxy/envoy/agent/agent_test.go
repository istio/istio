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
	"fmt"
	"net/http"
	"testing"

	"istio.io/istio/pilot/pkg/model"
)

func TestAgent(t *testing.T) {
	agents := []*Agent{
		{
			Config: Config{
				ServiceName: "serviceA",
				Ports: []PortConfig{
					{
						Name:     "http-1",
						Protocol: model.ProtocolHTTP,
					},
					{
						Name:     "http-2",
						Protocol: model.ProtocolHTTP,
					},
				},
			},
		},
		{
			Config: Config{
				ServiceName: "serviceB",
				Ports: []PortConfig{
					{
						Name:     "http",
						Protocol: model.ProtocolHTTP,
					},
				},
			},
		},
		{
			Config: Config{
				ServiceName: "serviceC",
				Ports: []PortConfig{
					{
						Name:     "http",
						Protocol: model.ProtocolHTTP,
					},
				},
			},
		},
		{
			Config: Config{
				ServiceName: "serviceD",
				Ports: []PortConfig{
					{
						Name:     "http-1",
						Protocol: model.ProtocolHTTP,
					},
					{
						Name:     "http-2",
						Protocol: model.ProtocolHTTP,
					},
					{
						Name:     "http-3",
						Protocol: model.ProtocolHTTP,
					},
				},
			},
		},
	}

	// Start all of the agents.
	for _, agent := range agents {
		if err := agent.Start(); err != nil {
			t.Fatal(err)
		}
		defer agent.Stop()
	}

	for _, agent := range agents {
		t.Run(agent.Config.ServiceName, func(t *testing.T) {
			for _, port := range agent.GetPorts() {
				t.Run(port.Config.Name, func(t *testing.T) {
					response, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d", port.ProxyPort))
					if err != nil {
						t.Fatal(err)
					}
					if response.StatusCode != 200 {
						t.Fatal(fmt.Errorf("unexpected status %d", response.StatusCode))
					}
				})
			}
		})
	}
}
