// Copyright 2018 Istio Authors
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

package probe

import (
	"errors"
	"testing"
)

func TestProbe(t *testing.T) {
	p := NewProbe()
	if err := p.IsAvailable(); err != errUninitialized {
		t.Errorf("want %v, got %v", errUninitialized, err)
	}

	dummy := errors.New("dummy")
	p.SetAvailable(dummy)
	if err := p.IsAvailable(); err != dummy {
		t.Errorf("want %v, got %v", dummy, err)
	}

	p.SetAvailable(nil)
	if err := p.IsAvailable(); err != nil {
		t.Errorf("want %v, got %v", nil, err)
	}

	p.SetAvailable(dummy)
	if err := p.IsAvailable(); err != dummy {
		t.Errorf("want %v, got %v", dummy, err)
	}
}
