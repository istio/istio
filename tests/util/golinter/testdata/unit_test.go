package testdata

import (
	"testing"
	"counter"
	"time"
)

func TestInvalidSkip(t *testing.T) {
	t.Skip("invalid skip without url to GitHub issue.")
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

func TestInvalidShort(t *testing.T) {
	counter.SetCount(0)
	if counter.Count() != 1 {
		t.Error("expected 1")
	}
	if counter.Count() != 2 {
		t.Error("expected 2")
	}

	if testing.Short() {
		t.Skip("skipping uint test in short mode.")
	}

	if counter.Count() != 3 {
		t.Error("expected 3")
	}
}

func TestInvalidSleep(t *testing.T) {
	counter.SetCount(0)
	if counter.Count() != 1 {
		t.Error("expected 1")
	}
	if counter.Count() != 2 {
		t.Error("expected 2")
	}
	time.Sleep(100 * time.Millisecond)

	if counter.Count() != 3 {
		t.Error("expected 3")
	}
}

func TestInvalidGoroutine(t *testing.T) {
	go counter.SetCount(3)
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


