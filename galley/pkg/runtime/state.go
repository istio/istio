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

package runtime

import (
	"github.com/google/uuid"
	"istio.io/istio/galley/pkg/api/distrib"
	"istio.io/istio/galley/pkg/model"
	"istio.io/istio/galley/pkg/model/common"
)

type MixerConfigState struct {
	versions map[model.ResourceKey]model.ResourceVersion
	parts map[model.ResourceKey]*PartialMixerConfigState
	names *common.Uniquifier
}

func newMixerConfigState() *MixerConfigState {
	return &MixerConfigState{
		versions: make(map[model.ResourceKey]model.ResourceVersion),
		parts: make(map[model.ResourceKey]*PartialMixerConfigState),
		names: common.NewUniquifier(),
	}
}

func (m *MixerConfigState) Generate() *distrib.MixerConfig {
	r := &distrib.MixerConfig{
		Id: uuid.New().String(),
	}

	for _, part := range m.parts {
		r.Instances = append(r.Instances, part.Instances...)
		r.Rules = append(r.Rules, part.Rules...)
	}

	return r
}

type PartialMixerConfigState struct {
	Rules []*distrib.Rule
	Instances []*distrib.Instance
}

