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
	"bytes"
	"fmt"
	"sort"

	"istio.io/istio/galley/pkg/api/distrib"
	"istio.io/istio/galley/pkg/api/service/dev"
	"istio.io/istio/galley/pkg/model/component"
	"istio.io/istio/galley/pkg/model/distributor"
	"istio.io/istio/galley/pkg/model/resource"
	"istio.io/istio/galley/pkg/runtime/common"
	"istio.io/istio/galley/pkg/runtime/generator"
	"istio.io/istio/galley/pkg/runtime/signature"
)

// Mixer state pertaining to a single Mixer instance.
type Mixer struct {
	// The unique id of the Mixer component that this config is destined for.
	destination component.InstanceID

	// The current version number of the bundle.
	version distributor.BundleVersion

	// The uniquifier for uniquifying the names.
	u *common.Uniquifier

	// The current set of fragment sets
	fragments map[resource.Key]*generator.MixerConfigFragment
	versions  map[resource.VersionedKey]struct{}
}

var _ distributor.Bundle = &Mixer{}

func newMixerState(componentID string) *Mixer {
	return &Mixer{
		destination: component.InstanceID{Kind: component.MixerKind, Name: componentID},
		u:           common.NewUniquifier(),
		fragments:   make(map[resource.Key]*generator.MixerConfigFragment),
		versions:    make(map[resource.VersionedKey]struct{}),
	}
}

// Destination is an implementation of distributor.Bundle.
func (m *Mixer) Destination() component.InstanceID {
	return m.destination
}

// GenerateManifest is an implementation of distributor.Bundle.
func (m *Mixer) GenerateManifest() *distrib.Manifest {

	man := &distrib.Manifest{
		ComponentType: string(component.MixerKind),
		ComponentId:   m.destination.Name,
		FragmentIds:   make([]string, 0, len(m.fragments)),
	}

	for _, f := range m.fragments {
		man.FragmentIds = append(man.FragmentIds, f.ID())
	}

	// Alpha sort strings for stable ordering.
	sort.Strings(man.FragmentIds)

	var buf bytes.Buffer
	for _, id := range man.FragmentIds {
		buf.WriteString(id)
	}

	man.Id = signature.OfStrings(man.FragmentIds...)

	return man
}

// GenerateFragments is an implementation of distributor.Bundle.
func (m *Mixer) GenerateFragments() []*distrib.Fragment {
	var result []*distrib.Fragment

	for _, f := range m.fragments {
		fr := f.Generate()
		result = append(result, fr)
	}

	return result
}

// String is an implementation of distributor.Bundle.
func (m *Mixer) String() string {
	return fmt.Sprintf("[state.Mixer](%s @%d, fragment#: %d)", m.destination, m.version, len(m.fragments))
}

func (m *Mixer) applyProducerService(key resource.VersionedKey, s *dev.ProducerService) bool {
	if _, ok := m.versions[key]; ok {
		return false
	}

	f := generator.GenerateMixerFragment(s, m.u)

	m.versions[key] = struct{}{}
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
