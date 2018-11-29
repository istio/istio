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

package stats_test

import (
	"bytes"
	"testing"

	. "github.com/onsi/gomega"

	. "istio.io/istio/pilot/cmd/pilot-agent/status/stats"
)

func TestMatchExact(t *testing.T) {
	g := NewGomegaWithT(t)

	var aval uint64
	var bval uint64
	var cval uint64
	var dval uint64

	p := &ProcessorChain{}
	p.Match(Exact("a")).Then(Set(&aval))
	p.Match(Exact("b")).Then(Set(&bval))
	p.Match(Exact("c")).Then(Set(&cval))
	p.Match(Exact("d")).Then(Set(&dval))

	// Note: d does not exist in the input doc.
	doc := "a:0\nb:1\nc:2\na2:3"
	if err := p.ProcessInput(toBuffer(doc)); err != nil {
		t.Fatal(err)
	}

	g.Expect(aval).To(Equal(uint64(0)))
	g.Expect(bval).To(Equal(uint64(1)))
	g.Expect(cval).To(Equal(uint64(2)))
	g.Expect(dval).To(Equal(uint64(0))) // D was not found.
}

func TestSetWithBadValueFails(t *testing.T) {
	g := NewGomegaWithT(t)

	doc := "a:bad\n"

	var aval uint64
	p := &ProcessorChain{}
	p.Match(Exact("a")).Then(Set(&aval))

	err := p.ProcessInput(toBuffer(doc))
	g.Expect(err).To(HaveOccurred())
}

func toBuffer(str string) *bytes.Buffer {
	return bytes.NewBuffer([]byte(str))
}
