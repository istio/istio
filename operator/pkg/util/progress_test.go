package util

import (
	"bytes"
	"io"
	"testing"
)

func TestProgressLog(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	testBuf := io.Writer(buf)
	testWriter = &testBuf
	expected := ""
	expect := func(e string) {
		t.Helper()
		// In buffer mode we don't overwrite old data, so we are constantly appending to the expected
		newExpected := expected + "\n" + e
		if newExpected != buf.String() {
			t.Fatalf("expected '%v', got '%v'", e, buf.String())
		}
		expected = newExpected
	}

	p := NewProgressLog()
	foo := p.NewComponent("foo")
	foo.ReportProgress()
	expect(`- Processing resources for components foo.`)

	bar := p.NewComponent("bar")
	bar.ReportProgress()
	// string buffer won't rewrite, so we append
	expect(`- Processing resources for components bar, foo.`)
	bar.ReportProgress()
	expect(`- Processing resources for components bar, foo.`)
	bar.ReportProgress()
	expect(`  Processing resources for components bar, foo.`)

	bar.ReportWaiting([]string{"deployment"})
	expect(`- Processing resources for components bar, foo. Waiting for deployment`)

	bar.ReportError("some error")
	expect(`✘ Component bar encountered an error: some error`)

	foo.ReportProgress()
	expect(`- Processing resources for components foo.`)

	foo.ReportFinished()
	expect(`✔ Component foo installed`)
}
