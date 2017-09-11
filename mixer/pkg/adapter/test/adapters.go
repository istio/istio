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

// Package test provides utility functions to assist in creating quality tests for
// adapters.
package test

import (
	gt "testing"

	"istio.io/mixer/pkg/adapter"
)

type fakeRegistrar struct {
	quotas        []adapter.QuotasBuilder
	attributesGen []adapter.AttributesGeneratorBuilder
}

func (r *fakeRegistrar) RegisterQuotasBuilder(b adapter.QuotasBuilder) {
	r.quotas = append(r.quotas, b)
}

func (r *fakeRegistrar) RegisterAttributesGeneratorBuilder(b adapter.AttributesGeneratorBuilder) {
	r.attributesGen = append(r.attributesGen, b)
}

// AdapterInvariants ensures that adapters implement expected semantics.
func AdapterInvariants(r adapter.RegisterFn, t *gt.T) {
	fr := &fakeRegistrar{}
	r(fr)

	count := len(fr.quotas)
	for _, b := range fr.quotas {
		testBuilder(b, t)
	}

	count += len(fr.attributesGen)
	for _, b := range fr.attributesGen {
		testBuilder(b, t)
	}

	if count == 0 {
		t.Error("Register() => adapter didn't register any builders")
	}
}

func testBuilder(b adapter.Builder, t *gt.T) {
	if b.Name() == "" {
		t.Error("Name() => all builders need names")
	}

	if b.Description() == "" {
		t.Errorf("Description() => builder '%s' doesn't provide a valid description", b.Name())
	}

	c := b.DefaultConfig()
	if err := b.ValidateConfig(c); err != nil {
		t.Errorf("ValidateConfig() => builder '%s' can't validate its default configuration: %v", b.Name(), err)
	}

	if err := b.Close(); err != nil {
		t.Errorf("Close() => builder '%s' fails to close when used with its default configuration: %v", b.Name(), err)
	}
}
