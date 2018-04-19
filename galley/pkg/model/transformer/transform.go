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

package transformer

import (
	"github.com/google/uuid"
	"istio.io/istio/galley/pkg/api"
	"istio.io/istio/galley/pkg/api/distrib"
	"istio.io/istio/galley/pkg/model/common"
)

type context struct {
	cfgs []*api.ServiceConfig

	out *distrib.MixerConfig

	names *common.Uniquifier
}

func Transform(cfgs []*api.ServiceConfig) (*distrib.MixerConfig, error) {
	c := &context {
		cfgs: cfgs,
		out: &distrib.MixerConfig{},
		names: common.NewUniquifier(),
	}

	c.out.Id = uuid.New().String()

	for _, cfg := range cfgs {
		if e := transform(c, cfg); e != nil {
			return nil, e
		}
	}

	return c.out, nil
}

func transform(c *context, cfg *api.ServiceConfig) error {
	for _, instance := range cfg.Instances {
		inst := &distrib.Instance{}

		c.names.Add(instance.Name)
		inst.Name = instance.Name
		inst.Template = instance.Template
		inst.Params = instance.Params

		c.out.Instances = append(c.out.Instances, inst)
	}

	for _, rule := range cfg.Rules {
			r := distrib.Rule{}

			r.Match = rule.Match
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
					inst.Name = c.names.Generate("_instance")
					inst.Template = instance.Template
					inst.Params = instance.Params
					c.out.Instances = append(c.out.Instances, inst)

					act.Instances = append(act.Instances, inst.Name)
				}

				r.Actions = append(r.Actions, act)
			}

			c.out.Rules = append(c.out.Rules, &r)
	}

	return nil
}