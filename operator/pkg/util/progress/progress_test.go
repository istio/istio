// Copyright Istio Authors
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

package progress

import (
	"bytes"
	"io"
	"testing"

	"istio.io/istio/operator/pkg/name"
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
			t.Fatalf("expected '%v', \ngot '%v'", newExpected, buf.String())
		}
		expected = newExpected
	}

	p := NewLog()
	cnp := name.PilotComponentName
	cnpo := name.UserFacingComponentName(cnp)
	cnb := name.IstioBaseComponentName
	cnbo := name.UserFacingComponentName(cnb)
	foo := p.NewComponent(string(cnp))
	foo.ReportProgress()
	expect(`- Processing resources for ` + cnpo + `.`)

	bar := p.NewComponent(string(cnb))
	bar.ReportProgress()
	// string buffer won't rewrite, so we append
	expect(`- Processing resources for ` + cnbo + `, ` + cnpo + `.`)
	bar.ReportProgress()
	bar.ReportProgress()

	bar.ReportWaiting([]string{"deployment"})
	expect(`- Processing resources for ` + cnbo + `, ` + cnpo + `. Waiting for deployment`)

	bar.ReportError("some error")
	expect(`✘ ` + cnbo + ` encountered an error: some error`)

	foo.ReportProgress()
	expect(`- Processing resources for ` + cnpo + `.`)

	foo.ReportFinished()
	expect(`✔ ` + cnpo + ` installed`)

	p.SetState(StatePruning)
	expect(`- Pruning removed resources`)

	p.SetState(StateComplete)
	expect(`✔ Installation complete`)

	p.SetState(StateUninstallComplete)
	expect(`✔ Uninstall complete`)
}
