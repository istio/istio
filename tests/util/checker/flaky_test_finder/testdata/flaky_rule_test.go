package testdata

import (
	"testing"

	"istio.io/istio/pkg/test/annotation"
)

func SetCount()       {}
func Count(n int) int { return n }

// nolint: testlinter
func TestIsMarkedFlaky1(t *testing.T) {
	SetCount()
	if Count(1) != 1 {
		t.Error("expected 1")
	}
	if Count(2) != 2 {
		t.Error("expected 2")
	}
	if Count(3) != 3 {
		t.Error("expected 3")
	}
	t.Skip("https://github.com/istio/istio/issues/6041")
	annotation.IsFlaky()
}

// nolint: testlinter
func TestIsMarkedFlaky2(t *testing.T) {
	SetCount()
	if Count(1) != 1 {
		t.Error("expected 1")
	}
	annotation.IsFlaky()
	if Count(2) != 2 {
		t.Error("expected 2")
	}
	if Count(3) != 3 {
		t.Error("expected 3")
	}
	t.Skip("https://github.com/istio/istio/issues/6041")
}

// nolint: testlinter
func TestIsNotMarkedFlaky(t *testing.T) {
	SetCount()
	if Count(1) != 1 {
		t.Error("expected 1")
	}
	if Count(2) != 2 {
		t.Error("expected 2")
	}
	if Count(3) != 3 {
		t.Error("expected 3")
	}
	t.Skip("https://github.com/istio/istio/issues/6041")
}
