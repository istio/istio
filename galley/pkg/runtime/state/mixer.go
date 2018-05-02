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

package state

import (
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"istio.io/istio/galley/pkg/api/distrib"
	"istio.io/istio/galley/pkg/api/service/dev"
	"istio.io/istio/galley/pkg/model/distributor"
	"istio.io/istio/galley/pkg/model/distributor/fragments"
	"istio.io/istio/galley/pkg/model/resource"
	"istio.io/istio/galley/pkg/runtime/common"
	"istio.io/istio/galley/pkg/runtime/generate"
)

type Mixer struct {
	version distributor.BundleVersion
	u       *common.Uniquifier

	fragments map[resource.Key]*MixerFragment
}

var _ distributor.Bundle = &Mixer{}

type MixerFragment struct {
	id string // TODO: Calculate id in a stable way.
	// The source configuration for this fragment
	source resource.VersionedKey

	instances []*distrib.Instance
	rules     []*distrib.Rule
}

func newMixerState() *Mixer {
	return &Mixer {
		u: common.NewUniquifier(),
		fragments: make(map[resource.Key]*MixerFragment),
	}
}

func (m *Mixer) GenerateManifest() *distrib.Manifest {
	man := &distrib.Manifest{
		Id: "TODO", // TODO: Generate a hash-based id.
		ComponentType: "Mixer", // TODO: consts for Mixer
		ComponentId: "", // TODO: Get ComponentId
		FragmentIds: make([]string, 0, len(m.fragments)),
	}

	for _, f := range m.fragments {
		man.FragmentIds = append(man.FragmentIds, f.id)
	}

	return man
}

func (m *Mixer) GetFragments() []*distrib.Fragment {
	var result []*distrib.Fragment

	for _, f := range m.fragments {
		for _, in := range f.instances {
			fr, err := buildFragment(f.id + "/" + in.Name, fragments.InstanceUrl, in)
			if err != nil {
				// TODO
				panic(err)
			}
			
			result = append(result, fr)
		}
		
		for _, r := range f.rules {
			fr, err := buildFragment(f.id + "/" , fragments.RuleUrl, r) // TODO
			if err != nil {
				// TODO
				panic(err)
			}

			result = append(result, fr)
		}
	}

	return result
}

func (m *Mixer) String() string {
	// TODO
	return ""
}

func (m *Mixer) applyProducerService(key resource.VersionedKey, s *dev.ProducerService) bool {
	f, ok := m.fragments[key.Key]
	if ok && f.source == key {
		return false
	}

	instances, rules := generate.MixerFragment(s, m.u)
	f = &MixerFragment{
		source:    key,
		instances: instances,
		rules:     rules,
	}

	m.fragments[key.Key] = f
	return true
}

func (m *Mixer) removeProducerService(key resource.VersionedKey) bool {
	if _, ok := m.fragments[key.Key]; !ok {
		return false
	}

	delete(m.fragments, key.Key)
	return true
}

func buildFragment(id string, url string, p proto.Message) (*distrib.Fragment, error) {
	value, err := proto.Marshal(p)
	if err != nil {
		return nil, err	
	}
	
	fr := &distrib.Fragment{
		Id: id,
		Content: &types.Any{
			TypeUrl: fragments.RuleUrl,
			Value: value,
		},
	}
	
	return fr, nil
}