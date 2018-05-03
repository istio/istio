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

package generator

import (
	"fmt"

	"istio.io/istio/galley/pkg/api/distrib"
	serviceconfig "istio.io/istio/galley/pkg/api/service/dev"
	"istio.io/istio/galley/pkg/runtime/common"
)

// GenerateMixerFragment generates a new Mixer config fragment from the supplied producer service.
func GenerateMixerFragment(cfg *serviceconfig.ProducerService, names *common.Uniquifier) *MixerConfigFragment {

	var instances []*distrib.Instance
	var rules []*distrib.Rule

	for _, instance := range cfg.Instances {
		inst := &distrib.Instance{}

		inst.Name = names.Discriminated(instance.Name, false)
		inst.Template = instance.Template
		inst.Params = instance.Params

		instances = append(instances, inst)
	}

	for _, rule := range cfg.Rules {
		r := distrib.Rule{}

		r.Match = rule.Match

		serviceConstraint := fmt.Sprintf("destination.service == %q", cfg.GetService().GetName())
		if r.Match == "" {
			r.Match = serviceConstraint
		} else {
			r.Match += " && " + serviceConstraint
		}

		for _, action := range rule.Actions {
			act := &distrib.Action{
				Handler: action.Handler,
			}

			for _, instance := range action.Instances {
				inst := &distrib.Instance{}

				if instance.Ref != "" {
					act.Instances = append(act.Instances, instance.Ref)
					continue
				}

				// Inline instance
				inst.Name = names.Discriminated("_instance", true)
				inst.Template = instance.Template
				inst.Params = instance.Params
				instances = append(instances, inst)

				act.Instances = append(act.Instances, inst.Name)
			}

			r.Actions = append(r.Actions, act)
		}

		rules = append(rules, &r)
	}

	return newMixerConfigFragment(instances, rules)
}
