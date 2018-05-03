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
	"github.com/gogo/protobuf/types"

	"istio.io/istio/galley/pkg/api/distrib"
	"istio.io/istio/galley/pkg/model/distributor"
	"istio.io/istio/galley/pkg/runtime/signature"
)

// MixerConfigFragment is a config fragment for Mixer.
type MixerConfigFragment struct {
	id        string
	Instances []*distrib.Instance
	Rules     []*distrib.Rule
}

func newMixerConfigFragment(instances []*distrib.Instance, rules []*distrib.Rule) *MixerConfigFragment {
	b := signature.GetBuilder()
	for _, in := range instances {
		b.EncodeProto(in)
	}
	for _, r := range rules {
		b.EncodeProto(r)
	}
	id := b.Calculate()
	b.Done()

	return &MixerConfigFragment{
		id:        id,
		Instances: instances,
		Rules:     rules,
	}
}

// ID of the fragment
func (m *MixerConfigFragment) ID() string {
	return m.id
}

// Generate a distributable fragment proto.
func (m *MixerConfigFragment) Generate() *distrib.Fragment {
	var content []*types.Any

	for _, in := range m.Instances {
		a, err := buildAny(distributor.InstanceURL, in)
		if err != nil {
			// TODO
			panic(err)
		}

		content = append(content, a)
	}

	for _, r := range m.Rules {
		a, err := buildAny(distributor.RuleURL, r)
		if err != nil {
			// TODO
			panic(err)
		}

		content = append(content, a)
	}

	return &distrib.Fragment{
		Id:      m.id,
		Content: content,
	}
}
