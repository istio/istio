// Copyright 2017 Istio Authors.
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

package convert

import (
	"fmt"

	"istio.io/api/networking/v1alpha3"
	"istio.io/api/routing/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

type egressConfig struct {
	name       string
	egressRule *v1alpha1.EgressRule
}

// EgressRules converts v1alpha1 egress rules to v1alpha3 service entries
func EgressRules(configs []model.Config) []model.Config {

	egressConfigs := make([]egressConfig, 0)
	for _, config := range configs {
		if config.Type == model.EgressRule.Type {
			egressConfigs = append(egressConfigs, egressConfig{
				name:       config.Name,
				egressRule: config.Spec.(*v1alpha1.EgressRule)})
		}
	}

	serviceEntries := make([]*v1alpha3.ServiceEntry, 0)
	for _, config := range egressConfigs {
		host := convertIstioService(config.egressRule.Destination)
		var addresses []*v1alpha3.Address
		if model.ValidateIPv4Subnet(host) == nil {
			addresses = []*v1alpha3.Address{{Address: &v1alpha3.Address_Ip{Ip: host}}}
			host = config.name
		}

		ports := make([]*v1alpha3.Port, 0)
		for _, egressPort := range config.egressRule.Ports {
			ports = append(ports, &v1alpha3.Port{
				Name:     fmt.Sprintf("%s-%d", egressPort.Protocol, egressPort.Port),
				Protocol: egressPort.Protocol,
				Number:   uint32(egressPort.Port),
			})
		}

		if config.egressRule.UseEgressProxy {
			log.Warnf("Use egress proxy field not supported")
		}

		serviceEntries = append(serviceEntries, &v1alpha3.ServiceEntry{
			Hosts:      []string{host},
			Addresses:  addresses,
			Ports:      ports,
			Resolution: v1alpha3.ServiceEntry_NONE,
		})
	}

	out := make([]model.Config, 0)
	for _, serviceEntry := range serviceEntries {
		out = append(out, model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      model.ServiceEntry.Type,
				Name:      serviceEntry.Hosts[0],
				Namespace: configs[0].Namespace,
				Domain:    configs[0].Domain,
			},
			Spec: serviceEntry,
		})
	}

	return out
}
