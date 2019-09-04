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
		diag.NewMessage(diag.Error, "test", nil, "test"),
	}

	cancelCh := make(chan struct{})

	su.Update(msgs)
	g.Expect(su.WaitForReport(cancelCh)).To(BeTrue())
	g.Expect(su.Get()).To(Equal(msgs))
}

func TestInMemoryStatusUpdaterWaitThenWrite(t *testing.T) {
	g := NewGomegaWithT(t)

	su := &InMemoryStatusUpdater{}

	msgs := diag.Messages{
		diag.NewMessage(diag.Error, "test", nil, "test"),
	}

	cancelCh := make(chan struct{})

	go func() {
		g.Expect(su.WaitForReport(cancelCh)).To(BeTrue())
	}()

	su.Update(msgs)
	g.Expect(su.Get()).To(Equal(msgs))
}
