package testdata

import (
	"testing"
	"time"
)

// whitelist(https://github.com/istio/istio/issues/6041):no_goroutine,short_skip.
func TestWhitelistRules(t *testing.T) {
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

// whitelist(https://github.com/istio/istio/issues/6041):no_short,no_sleep,skip_issue.
func TestMain(t *testing.T) {
}
