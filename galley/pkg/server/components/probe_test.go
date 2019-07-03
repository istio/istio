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

package components

import (
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"istio.io/pkg/probe"
)

func TestProbe_Basic(t *testing.T) {
	g := NewGomegaWithT(t)

	f, err := ioutil.TempFile(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())

	o := &probe.Options{
		UpdateInterval: time.Second,
		Path:           path.Join(os.TempDir(), f.Name()),
	}
	p := NewProbe(o)
	err = p.Start()
	g.Expect(err).To(BeNil())

	g.Expect(p.Controller()).NotTo(BeNil())

	p.Stop()
}

func TestProbe_Disabled(t *testing.T) {
	g := NewGomegaWithT(t)

	o := &probe.Options{}
	p := NewProbe(o)
	err := p.Start()
	g.Expect(err).To(BeNil())

	g.Expect(p.Controller()).To(BeNil())

	p.Stop()
}

func TestProbe_DoubleStop(t *testing.T) {
	g := NewGomegaWithT(t)

	f, err := ioutil.TempFile(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())

	o := &probe.Options{
		UpdateInterval: time.Second,
		Path:           path.Join(os.TempDir(), f.Name()),
	}
	p := NewProbe(o)
	err = p.Start()
	g.Expect(err).To(BeNil())

	g.Expect(p.Controller()).NotTo(BeNil())

	p.Stop()
	p.Stop()
}
