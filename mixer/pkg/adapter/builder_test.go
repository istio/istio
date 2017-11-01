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

package adapter

import (
	"testing"

	rpc "github.com/googleapis/googleapis/google/rpc"
)

func TestDefaultBuilder(t *testing.T) {
	c1 := &rpc.Status{}

	b := NewDefaultBuilder("NAME", "DESC", c1)

	if b.Name() != "NAME" {
		t.Errorf("Got %s, expecting NAME", b.Name())
	}

	if b.Description() != "DESC" {
		t.Errorf("Got %s, expecting DESC", b.Description())
	}

	if err := b.ValidateConfig(c1); err != nil {
		t.Errorf("Got error %v, expected nil", err)
	}

	c2 := b.DefaultConfig()
	if c1 != c2 {
		t.Error("Expecting to get default config back, got something else")
	}

	if err := b.Close(); err != nil {
		t.Errorf("Expected success, got %v", err)
	}
}
