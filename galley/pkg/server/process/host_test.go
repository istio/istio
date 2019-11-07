// Copyright 2019 Istio Authors
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

package process

import (
	"errors"
	"testing"

	. "github.com/onsi/gomega"
)

func TestHost_Basic(t *testing.T) {
	g := NewGomegaWithT(t)

	c1 := &fakeComponent{}
	var h Host
	h.Add(c1)

	err := h.Start()
	g.Expect(err).To(BeNil())
	g.Expect(c1.started).To(BeTrue())

	h.Stop()
	g.Expect(c1.started).To(BeFalse())
}

func TestHost_AddAfterStart(t *testing.T) {
	g := NewGomegaWithT(t)

	var h Host
	err := h.Start()
	g.Expect(err).To(BeNil())
	defer h.Stop()

	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()

	c1 := &fakeComponent{}
	h.Add(c1)
}

func TestHost_DoubleStart(t *testing.T) {
	g := NewGomegaWithT(t)

	c1 := &fakeComponent{}
	var h Host
	h.Add(c1)

	err := h.Start()
	g.Expect(err).To(BeNil())
	g.Expect(c1.started).To(BeTrue())

	defer h.Stop()

	err = h.Start()
	g.Expect(err).NotTo(BeNil())
}

func TestHost_StartFailure(t *testing.T) {
	g := NewGomegaWithT(t)

	c1 := &fakeComponent{}
	c2 := &fakeComponent{startErr: errors.New("some err")}
	c3 := &fakeComponent{}
	var h Host
	h.Add(c1)
	h.Add(c2)
	h.Add(c3)

	err := h.Start()
	g.Expect(err).NotTo(BeNil())
	g.Expect(c1.started).To(BeFalse())
	g.Expect(c2.started).To(BeFalse())
	g.Expect(c2.started).To(BeFalse())
}

type fakeComponent struct {
	started  bool
	startErr error
}

var _ Component = &fakeComponent{}

func (f *fakeComponent) Start() error {
	if f.startErr != nil {
		return f.startErr
	}

	f.started = true
	return nil
}

func (f *fakeComponent) Stop() {
	f.started = false
}
