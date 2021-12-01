package controllers

import (
	"testing"

	"go.uber.org/atomic"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/test/util/retry"
)

func TestQueue(t *testing.T) {
	handles := atomic.NewInt32(0)
	q := NewQueue(WithName("custom"), WithReconciler(func(name types.NamespacedName) error {
		handles.Inc()
		return nil
	}))
	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
	})
	q.Add(types.NamespacedName{Name: "something"})
	go q.Run(stop)
	retry.UntilOrFail(t, q.HasSynced)
	if got := handles.Load(); got != 1 {
		t.Fatalf("expected 1 handle, got %v", got)
	}
}
