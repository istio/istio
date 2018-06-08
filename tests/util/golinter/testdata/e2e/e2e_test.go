package testdata

import (
	"counter"
	"testing"
)

func TestE2eInvalidSkip(t *testing.T) {
	t.Skip("invalid t.Skip without url to GitHub issue.")
	counter.SetCount(0)
	if counter.Count() != 1 {
		t.Error("expected 1")
	}
	if counter.Count() != 2 {
		t.Error("expected 2")
	}
	if counter.Count() != 3 {
		t.Error("expected 3")
	}
	t.Skip("https://github.com/istio/istio/issues/6041")
}

func TestE2eNoShort(t *testing.T) {
	counter.SetCount(0)
	if counter.Count() != 1 {
		t.Error("expected 1")
	}
	if counter.Count() != 2 {
		t.Error("expected 2")
	}

	if counter.Count() != 3 {
		t.Error("expected 3")
	}
}

func TestE2eSkipAtTop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}
	counter.SetCount(3)
	if counter.Count2() != 5 {
		t.Error("expected 5")
	}
	if counter.Count2() != 7 {
		t.Error("expected 7")
	}

	if counter.Count2() != 9 {
		t.Error("expected 9")
	}
}

func TestE2eSkipAtTop2(t *testing.T) {
	if !testing.Short() {
		counter.SetCount(3)
		if counter.Count2() != 5 {
			t.Error("expected 5")
		}
		if counter.Count2() != 7 {
			t.Error("expected 7")
		}

		if counter.Count2() != 9 {
			t.Error("expected 9")
		}
	}
}
