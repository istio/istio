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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"istio.io/istio/galley/pkg/api/distrib"
	"istio.io/istio/galley/pkg/api/service/dev"
	"istio.io/istio/galley/pkg/model/component"
	"istio.io/istio/galley/pkg/model/distributor"
	"istio.io/istio/galley/pkg/model/resource"
	"istio.io/istio/galley/pkg/runtime/common"
	"istio.io/istio/galley/pkg/runtime/generate"
)

// Mixer state pertaining to a single Mixer instance.
type Mixer struct {
	// The unique id of the Mixer component that this config is destined for.
	destination component.InstanceId

	// The current version number of the bundle.
	version distributor.BundleVersion

	// The uniquifier for uniquifying the names.
	u *common.Uniquifier

	// The current set of fragment sets
	fragments map[resource.Key]*mixerFragmentSet
}

var _ distributor.Bundle = &Mixer{}

type mixerFragmentSet struct {
	id string
	// The source configuration for this fragment
	source resource.VersionedKey

	instances []*distrib.Instance
	rules     []*distrib.Rule
}

func newMixerState(componentId string) *Mixer {
	return &Mixer{
		destination: component.InstanceId{Kind: component.MixerKind, Name: componentId},
		u:           common.NewUniquifier(),
		fragments:   make(map[resource.Key]*mixerFragmentSet),
	}
}

func (m *Mixer) Destination() component.InstanceId {
	return m.destination
}

func (m *Mixer) GenerateManifest() *distrib.Manifest {

	man := &distrib.Manifest{
		ComponentType: string(component.MixerKind),
		ComponentId:   m.destination.Name,
		FragmentIds:   make([]string, 0, len(m.fragments)),
	}

	for _, f := range m.fragments {
		man.FragmentIds = append(man.FragmentIds, f.id)
	}

	// Alpha sort strings for stable ordering.
	sort.Strings(man.FragmentIds)

	var buf bytes.Buffer
	for _, id := range man.FragmentIds {
		buf.WriteString(id)
	}

	man.Id = calculateSignature(&buf)

	return man
}

func (m *Mixer) GenerateFragments() []*distrib.Fragment {
	var result []*distrib.Fragment

	for _, f := range m.fragments {
		var content []*types.Any

		for _, in := range f.instances {
			a, err := buildAny(distributor.InstanceUrl, in)
			if err != nil {
				// TODO
				panic(err)
			}

			content = append(content, a)
		}

		for _, r := range f.rules {
			a, err := buildAny(distributor.RuleUrl, r)
			if err != nil {
				// TODO
				panic(err)
			}

			content = append(content, a)
		}

		fr := &distrib.Fragment{
			Id:      f.id,
			Content: content,
		}

		result = append(result, fr)
	}

	return result
}

func (m *Mixer) String() string {
	return fmt.Sprintf("[state.Mixer](%s @%d, fragment#: %d)", m.destination, m.version, len(m.fragments))
}

func (m *Mixer) applyProducerService(key resource.VersionedKey, s *dev.ProducerService) bool {
	f, ok := m.fragments[key.Key]
	if ok && f.source == key {
		return false
	}

	instances, rules := generate.MixerFragment(s, m.u)
	f = newMixerFragmentSet(key, instances, rules)

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

func newMixerFragmentSet(source resource.VersionedKey, instances []*distrib.Instance, rules []*distrib.Rule) *mixerFragmentSet {
	f := &mixerFragmentSet{
		source:    source,
		instances: instances,
		rules:     rules,
	}

	var buf bytes.Buffer

	for _, in := range f.instances {
		encode(&buf, in)
	}

	for _, r := range f.rules {
		encode(&buf, r)
	}

	id := calculateSignature(&buf)
	f.id = id
	return f
}

func buildAny(url string, p proto.Message) (*types.Any, error) {
	value, err := proto.Marshal(p)
	if err != nil {
		return nil, err
	}

	return &types.Any{
		TypeUrl: url,
		Value:   value,
	}, nil
}

func encode(b *bytes.Buffer, p proto.Message) {
	// Marshal from proto to json bytes
	m := jsonpb.Marshaler{}
	str, err := m.MarshalToString(p)
	if err != nil {
		// TODO: Handle the error case
		panic(err)
	}
	b.WriteString(str)
}

func calculateSignature(b *bytes.Buffer) string {
	signature := sha256.Sum256(b.Bytes())

	dst := make([]byte, hex.EncodedLen(len(signature)))
	hex.Encode(dst, signature[:])
	return string(dst)
}
