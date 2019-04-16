// Copyright 2017 Istio Authors
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

package store

import (
	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pkg/log"
)

type perKindValidateFunc func(br *BackEndResource) error

type perKindValidator struct {
	pbSpec proto.Message
}

func (pv *perKindValidator) validateAndConvert(key Key, br *BackEndResource, res *Resource) error {
	res.Spec = proto.Clone(pv.pbSpec)
	return convert(key, br.Spec, res.Spec)
}

// validator provides the default structural validation with delegating
// an external validator for the referential integrity.
type validator struct {
	externalValidator Validator
	perKindValidators map[string]*perKindValidator
}

// NewValidator creates a default validator which validates the structure through registered
// kinds and referential integrity through ev.
func NewValidator(ev Validator, kinds map[string]proto.Message) BackendValidator {
	vs := make(map[string]*perKindValidator, len(kinds))
	for k, pb := range kinds {
		vs[k] = &perKindValidator{pb}
	}
	return &validator{
		externalValidator: ev,
		perKindValidators: vs,
	}
}

func (v *validator) Validate(bev *BackendEvent) error {
	pkv, ok := v.perKindValidators[bev.Key.Kind]
	if !ok {
		// Pass unrecognized kinds -- they should be validated by somewhere else.
		log.Debugf("unrecognized kind %s is requested to validate", bev.Key.Kind)
		return nil
	}
	ev := &Event{
		Type: bev.Type,
		Key:  bev.Key,
	}
	if bev.Type == Update {
		ev.Value = &Resource{Metadata: bev.Value.Metadata}
		if err := pkv.validateAndConvert(bev.Key, bev.Value, ev.Value); err != nil {
			return err
		}
	}
	if v.externalValidator == nil {
		return nil
	}
	return v.externalValidator.Validate(ev)
}
