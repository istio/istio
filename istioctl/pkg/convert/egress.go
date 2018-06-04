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
	namespace  string
	domain     string
	egressRule *v1alpha1.EgressRule
}

// EgressRules converts v1alpha1 egress rules to v1alpha3 service entries
func EgressRules(configs []model.Config) []model.Config {

	egressConfigs := make([]egressConfig, 0)
	out := make([]model.Config, 0)
	for _, config := range configs {
		if config.Type == model.EgressRule.Type {
			rule := egressConfig{
				name:       config.Name,
				namespace:  config.Namespace,
				domain:     config.Domain,
				egressRule: config.Spec.(*v1alpha1.EgressRule)}
			if len(rule.namespace) == 0 {
				rule.namespace = "default"
			}

			if len(rule.domain) == 0 {
				rule.domain = "cluster.local"
			}

			egressConfigs = append(egressConfigs, rule)
		}
	}

	for _, config := range egressConfigs {
		host := convertIstioService(config.egressRule.Destination)
		var addresses []string
		if model.ValidateIPv4Subnet(host) == nil {
			addresses = []string{host}
			// hosts cannot have short names
			host = fmt.Sprintf("%s.%s.svc.%s", config.name, config.namespace, config.domain)
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

		out = append(out, model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      model.ServiceEntry.Type,
				Name:      config.name,
				Namespace: config.namespace,
				Domain:    config.domain,
			},
			Spec: &v1alpha3.ServiceEntry{
				Hosts:      []string{host},
				Addresses:  addresses,
				Ports:      ports,
				Resolution: v1alpha3.ServiceEntry_NONE,
			},
		})
	}

	return out
}
