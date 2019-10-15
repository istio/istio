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

package snapshotter

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/analysis/diag"
)

func TestInMemoryStatusUpdaterWriteThenWait(t *testing.T) {
	g := NewGomegaWithT(t)

	su := &InMemoryStatusUpdater{}

	msgs := diag.Messages{
		diag.NewMessage(diag.NewMessageType(diag.Error, "test", "test"), nil),
	}

	cancelCh := make(chan struct{})

	su.Update(msgs)
	g.Expect(su.WaitForReport(cancelCh)).To(BeTrue())
	g.Expect(su.Get()).To(Equal(msgs))
}

func TestInMemoryStatusUpdaterWriteNothingThenWait(t *testing.T) {
	g := NewGomegaWithT(t)

	su := &InMemoryStatusUpdater{}

	var msgs diag.Messages

	cancelCh := make(chan struct{})

	su.Update(msgs)
	g.Expect(su.WaitForReport(cancelCh)).To(BeTrue())
	g.Expect(su.Get()).To(Equal(msgs))
}

func TestInMemoryStatusUpdaterWaitThenWrite(t *testing.T) {
	g := NewGomegaWithT(t)

	su := &InMemoryStatusUpdater{}

	msgs := diag.Messages{
		diag.NewMessage(diag.NewMessageType(diag.Error, "test", "test"), nil),
	}

	cancelCh := make(chan struct{})

	go func() {
		g.Expect(su.WaitForReport(cancelCh)).To(BeTrue())
	}()

	su.Update(msgs)
	g.Expect(su.Get()).To(Equal(msgs))
}
