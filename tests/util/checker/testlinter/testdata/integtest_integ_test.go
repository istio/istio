package testdata

import (
	"testing"
)

func SetCount()       {}
func Count(n int) int { return n }

// nolint: testlinter
func TestIntegInvalidSkip(t *testing.T) {
	t.Skip("invalid t.Skip without url to GitHub issue.")
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

// nolint: testlinter
func TestIntegNoShort(t *testing.T) {
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
}

// nolint: testlinter
func TestIntegSkipAtTop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}
	SetCount()
	if Count(5) != 5 {
		t.Error("expected 5")
	}
	if Count(7) != 7 {
		t.Error("expected 7")
	}

	if Count(9) != 9 {
		t.Error("expected 9")
	}
}

// nolint: testlinter
func TestIntegSkipAtTop2(t *testing.T) {
	if !testing.Short() {
		SetCount()
		if Count(5) != 5 {
			t.Error("expected 5")
		}
		if Count(7) != 7 {
			t.Error("expected 7")
		}

		if Count(9) != 9 {
			t.Error("expected 9")
		}
	}
}
