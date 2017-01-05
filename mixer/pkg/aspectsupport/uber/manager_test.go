// Copyright 2016 Google Inc.
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

package uber

import (
	"strings"
	"testing"

	istioconfig "istio.io/api/istio/config/v1"

	"istio.io/mixer/pkg/aspectsupport"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

type (
	fakereg struct {
		RegistryQuerier
	}

	fakemgr struct {
		kind string
		aspectsupport.Manager
	}

	fakebag struct {
		attribute.Bag
	}

	fakeevaluator struct {
		expr.Evaluator
	}
)

func (m *fakemgr) Kind() string {
	return m.kind
}

func TestManager(t *testing.T) {
	r := &fakereg{}
	mgrs := []aspectsupport.Manager{&fakemgr{kind: "k1"}, &fakemgr{kind: "k2"}}
	m := NewManager(r, mgrs)
	cfg := &aspectsupport.CombinedConfig{
		Aspect:  &istioconfig.Aspect{},
		Adapter: &istioconfig.Adapter{},
	}
	attrs := &fakebag{}
	mapper := &fakeevaluator{}
	if _, err := m.Execute(cfg, attrs, mapper); err != nil {
		if !strings.Contains(err.Error(), "could not find aspect manager") {
			t.Error("excute errored out: ", err)
		}

	}
}
