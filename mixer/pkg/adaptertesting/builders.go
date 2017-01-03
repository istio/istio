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

package adaptertesting

import (
	"testing"

	"istio.io/mixer/pkg/adapter"
)

// TestBuilderInvariants ensures that builders implement expected semantics.
func TestBuilderInvariants(b adapter.Adapter, t *testing.T) {
	if b.Name() == "" {
		t.Error("All builders need names")
	}

	if b.Description() == "" {
		t.Error("All builders need descriptions")
	}

	bc := b.DefaultAdapterConfig()
	if err := b.ValidateAdapterConfig(bc); err != nil {
		t.Errorf("Default adapter config is expected to validate correctly: %v", err)
	}

	ac := b.DefaultAspectConfig()
	if err := b.ValidateAspectConfig(ac); err != nil {
		t.Errorf("Default adapter config is expected to validate correctly: %v", err)
	}

	if err := b.Configure(bc); err != nil {
		t.Errorf("Should be able to configure the adapter using the default config: %v", err)
	}

	if _, err := b.NewAspect(ac); err != nil {
		t.Errorf("Should be able to create an adapter with the default config: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Errorf("Should not fail Close with default config: %v", err)
	}
}
