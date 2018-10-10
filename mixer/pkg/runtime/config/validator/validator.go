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

package validator

import (
	"fmt"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"
	multierror "github.com/hashicorp/go-multierror"

	cpb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/lang/checker"
	"istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/runtime/config/constant"
	"istio.io/istio/mixer/pkg/template"
)

// Validator offers semantic validation of the config changes.
type Validator struct {
	handlerBuilders map[string]adapter.HandlerBuilder
	templates       map[string]*template.Info
	donec           chan struct{}
	e               *config.Ephemeral
}

// NewValidator creates a new store.Validator instance which validates runtime semantics of
// the configs.
func NewValidator(tc checker.TypeChecker, s store.Store,
	adapterInfo map[string]*adapter.Info, templateInfo map[string]*template.Info) (store.Validator, error) {
	kinds := config.KindMap(adapterInfo, templateInfo)
	if err := s.Init(kinds); err != nil {
		return nil, err
	}
	data, ch, err := store.StartWatch(s)
	if err != nil {
		return nil, err
	}
	hb := make(map[string]adapter.HandlerBuilder, len(adapterInfo))
	for k, ai := range adapterInfo {
		hb[k] = ai.NewBuilder()
	}
	configData := make(map[store.Key]proto.Message, len(data))
	manifests := map[store.Key]*cpb.AttributeManifest{}
	for k, obj := range data {
		if k.Kind == constant.AttributeManifestKind {
			manifests[k] = obj.Spec.(*cpb.AttributeManifest)
		}
		configData[k] = obj.Spec
	}
	e := config.NewEphemeral(templateInfo, adapterInfo)
	v := &Validator{
		handlerBuilders: hb,
		templates:       templateInfo,
		donec:           make(chan struct{}),
		e:               e,
	}
	v.e.SetState(data)
	go store.WatchChanges(ch, v.donec, time.Second, v.e.ApplyEvent)
	return v, nil
}

// Stop stops the validator.
func (v *Validator) Stop() {
	close(v.donec)
}

// Validate implements store.Validator interface.
func (v *Validator) Validate(ev *store.Event) error {
	// get old state so we can revert in case of validation error.
	oldEntryVal, exists := v.e.GetEntry(ev)

	v.e.ApplyEvent([]*store.Event{ev})
	s, err := v.e.BuildSnapshot()
	// now validate snapshot as a whole
	if err == nil {
		err = validateHandlers(s)
	}

	if err != nil {
		reverseEvent := *ev
		if exists {
			reverseEvent.Value = oldEntryVal
			reverseEvent.Type = store.Update
		} else if ev.Type == store.Update { // didn't existed before.
			reverseEvent.Type = store.Delete
		}
		v.e.ApplyEvent([]*store.Event{&reverseEvent})
	}

	return err
}

func validateHandlers(s *config.Snapshot) error {
	var errs error
	instancesByHandler := config.GetInstancesGroupedByHandlers(s)
	for handler, instances := range instancesByHandler {
		if _, err := config.ValidateBuilder(handler, instances, s.Templates); err != nil {
			insts := make([]string, 0)
			for _, inst := range instances {
				insts = append(insts, inst.Name)
			}
			errs = multierror.Append(errs, fmt.Errorf("error handler[%s]/instances[%s]: %v",
				handler.Name, strings.Join(insts, ","), err))
		}
	}
	return errs
}
