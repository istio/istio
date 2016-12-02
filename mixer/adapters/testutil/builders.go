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

package testutil

import (
	"testing"

	"istio.io/mixer/adapters"
)

// TestBuilderInvariants ensures that builders implement expected semantics.
func TestBuilderInvariants(b adapters.Builder, t *testing.T) {
	if b.Name() == "" {
		t.Error("All builders need names")
	}

	if b.Description() == "" {
		t.Error("All builders need descriptions")
	}

	bc := b.DefaultBuilderConfig()
	if err := b.ValidateBuilderConfig(bc); err != nil {
		t.Error("Default builder config is expected to validate correctly")
	}

	ac := b.DefaultAdapterConfig()
	if err := b.ValidateAdapterConfig(ac); err != nil {
		t.Error("Default adapter config is expected to validate correctly")
	}

	if err := b.Configure(bc); err != nil {
		t.Error("Should be able to configure the builder using the default config")
	}

	if _, err := b.NewAdapter(ac); err != nil {
		t.Error("Should be able to create an adapter with the default config")
	}

	if err := b.Close(); err != nil {
		t.Error("Should not fail Close with default config, otherwise what is this world coming to?")
	}
}
