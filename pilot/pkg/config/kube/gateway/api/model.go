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

package api

import (
	"encoding/json"

	"istio.io/istio/pkg/config/constants"
)

// GatewayClassConfiguration defines the configuration for a gateway class
// Currently, this is referenced via configmap. In the future, it may be its own CRD
type GatewayClassConfiguration struct {
	Workload GatewayWorkload `json:"workload"`
}

// GatewayWorkload references a set of workloads
type GatewayWorkload struct {
	// Selector will cause gateways created in this class to only apply to gateway workloads (Pods,
	// in Kubernetes) matching these label.
	Selector map[string]string `json:"selector"`
	// Namespace scopes the selector to a specific namespace
	// This cannot be implemented in the current Istio API
	// Namespace string            `json:"namespace"`
}

func DefaultGatewayClassConfiguration() GatewayClassConfiguration {
	return GatewayClassConfiguration{
		Workload: GatewayWorkload{
			Selector: map[string]string{
				constants.IstioLabel: "ingressgateway",
			},
		},
	}
}

func GatewayClassConfigurationFromMap(data map[string]string) (GatewayClassConfiguration, error) {
	gwc := GatewayClassConfiguration{}
	js, err := json.Marshal(data)
	if err != nil {
		return gwc, err
	}
	if err := json.Unmarshal(js, &gwc); err != nil {
		return gwc, err
	}
	return gwc, nil
}
