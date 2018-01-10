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
	"fmt"
	"testing"
)

type dummyController struct {
	controllerBase
	registerCount int
	changeCount   int
}

func (d *dummyController) register(p *Probe, initial error) {
	d.registerCount++
	d.controllerBase.registerBase(p)
}

func (d *dummyController) onChange(p *Probe, status error) {
	d.changeCount++
	d.update(p, status)
}

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

func TestProbeSignalsController(t *testing.T) {
	p := NewProbe()
	c := dummyController{controllerBase: controllerBase{statuses: map[*Probe]error{}}}
	p.RegisterProbe(&c, "test")
	if s := fmt.Sprintf("%s", p); s != "test" {
		t.Errorf(`want "test", got %s`, p)
	}

	if c.registerCount != 1 {
		t.Errorf("want %v, got %v", 1, c.registerCount)
	}
	if c.changeCount != 0 {
		t.Errorf("want %v, got %v", 0, c.changeCount)
	}

	dummy := errors.New("dummy")
	for i, cc := range []struct {
		status      error
		changeCount int
	}{
		{dummy, 1},
		{nil, 2},
		{nil, 2},
		{dummy, 3},
		{dummy, 3},
		{errors.New("dummy2"), 4},
		{nil, 5},
	} {
		p.SetAvailable(cc.status)
		if c.registerCount != 1 {
			t.Errorf("want %v, got %v at %d", 1, c.registerCount, i)
		}
		if c.changeCount != cc.changeCount {
			t.Errorf("want %v, got %v at %d", cc.changeCount, c.changeCount, i)
		}
	}
}
