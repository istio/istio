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

package validate

import (
	"github.com/gogo/protobuf/proto"

	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	runtimeConfig "istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/template"
	generatedTmplRepo "istio.io/istio/mixer/template"
	"istio.io/pkg/log"
)

// validator provides the default structural validation
type validator struct {
	kinds  map[string]proto.Message
	config *runtimeConfig.Ephemeral
}

// NewValidator creates a default validator which validates the structure through registered
// kinds and optionally referential integrity and expressions if stateful verification is enabled.
func NewValidator(adapters map[string]*adapter.Info, templates map[string]*template.Info, stateful bool) store.BackendValidator {
	out := &validator{
		kinds: runtimeConfig.KindMap(adapters, templates),
	}
	if stateful {
		out.config = runtimeConfig.NewEphemeral(templates, adapters)
	}
	return out
}

// NewDefaultValidator creates a validator using compiled templates and adapter descriptors.
// It does not depend on actual handler/builder interfaces from the compiled-in adapters.
func NewDefaultValidator(stateful bool) store.BackendValidator {
	info := generatedTmplRepo.SupportedTmplInfo
	templates := make(map[string]*template.Info, len(info))
	for k := range info {
		t := info[k]
		templates[k] = &t
	}
	adapters := metadata.InfoMap()
	return NewValidator(adapters, templates, stateful)
}

func (v *validator) Validate(bev *store.BackendEvent) error {
	_, ok := v.kinds[bev.Key.Kind]
	if !ok {
		// Pass unrecognized kinds -- they should be validated by somewhere else.
		log.Debugf("unrecognized kind %s is requested to validate", bev.Key.Kind)
		return nil
	}

	ev, err := store.ConvertValue(*bev, v.kinds)
	if err != nil {
		return err
	}

	// optional deep validation
	if v.config != nil {
		v.config.ApplyEvent([]*store.Event{&ev})
		if _, err = v.config.BuildSnapshot(); err != nil {
			return err
		}
	}

	return nil
}

func (v *validator) SupportsKind(kind string) bool {
	_, exists := v.kinds[kind]
	return exists
}
