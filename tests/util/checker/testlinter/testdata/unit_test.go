package testdata

import (
	"testing"
	"time"
)

func TestInvalidSkip(t *testing.T) {
	t.Skip("invalid skip without url to GitHub issue.")
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

func TestInvalidShort(t *testing.T) {
	SetCount()
	if Count(1) != 1 {
		t.Error("expected 1")
	}
	if Count(2) != 2 {
		t.Error("expected 2")
	}

	if testing.Short() {
		t.Skip("skipping uint test in short mode.")
	}

	if Count(3) != 3 {
		t.Error("expected 3")
	}
}

func TestInvalidSleep(t *testing.T) {
	SetCount()
	if Count(1) != 1 {
		t.Error("expected 1")
	}
	if Count(2) != 2 {
		t.Error("expected 2")
	}
	time.Sleep(100 * time.Millisecond)

	if Count(3) != 3 {
		t.Error("expected 3")
	}
}

func TestInvalidGoroutine(t *testing.T) {
	go SetCount()
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

// whitelist(https://github.com/istio/istio/issues/6041):*.
func TestWhitelistAll(t *testing.T) {
	t.Skip("invalid skip without url to GitHub issue.")
	go SetCount()
	if Count(1) != 1 {
		t.Error("expected 1")
	}
	if testing.Short() {
		t.Skip("skipping uint test in short mode.")
	}
	if Count(3) != 3 {
		t.Error("expected 3")
	}
	time.Sleep(100 * time.Millisecond)
	t.Skip("https://github.com/istio/istio/issues/6041")
}

// whitelist(https://github.com/istio/istio/issues/6041):no_goroutine.
func TestWhitelistNoGoroutine(t *testing.T) {
	t.Skip("invalid skip without url to GitHub issue.")
	go SetCount()
	if Count(1) != 1 {
		t.Error("expected 1")
	}
	if testing.Short() {
		t.Skip("skipping uint test in short mode.")
	}
	if Count(3) != 3 {
		t.Error("expected 3")
	}
	time.Sleep(100 * time.Millisecond)
	t.Skip("https://github.com/istio/istio/issues/6041")
}

// whitelist(https://github.com/istio/istio/issues/6041):no_goroutine,short_skip.
func TestWhitelistShortSkip(t *testing.T) {
	t.Skip("invalid skip without url to GitHub issue.")
	go SetCount()
	if Count(1) != 1 {
		t.Error("expected 1")
	}
	if testing.Short() {
		t.Skip("skipping uint test in short mode.")
	}
	if Count(3) != 3 {
		t.Error("expected 3")
	}
	time.Sleep(100 * time.Millisecond)
	t.Skip("https://github.com/istio/istio/issues/6041")
}
