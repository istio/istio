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
	"errors"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pkg/log"
)

type perKindValidateFunc func(br *BackEndResource) error

type perKindValidator struct {
	pbSpec   proto.Message
	validate perKindValidateFunc
}

func (pv *perKindValidator) validateAndConvert(key Key, br *BackEndResource, res *Resource) error {
	if pv.validate != nil {
		if err := pv.validate(br); err != nil {
			return err
		}
	}
	res.Spec = proto.Clone(pv.pbSpec)
	return convert(key, br.Spec, res.Spec)
}

func validateRule(br *BackEndResource) error {
	_, matchExists := br.Spec[matchField]
	_, selectorExists := br.Spec[selectorField]
	if !matchExists && selectorExists {
		return errors.New("field 'selector' is deprecated, use 'match' instead")
	}
	if selectorExists {
		log.Warnf("Deprecated field 'selector' used in %s. Use 'match' instead.", br.Metadata.Name)
	}
	return nil
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
		var validateFunc perKindValidateFunc
		if k == ruleKind {
			validateFunc = validateRule
		}
		vs[k] = &perKindValidator{pb, validateFunc}
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
