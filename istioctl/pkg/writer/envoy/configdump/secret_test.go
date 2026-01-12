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

package configdump

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"

	"istio.io/istio/pkg/test/util/assert"
)

func TestSDSWriter_ValidCert(t *testing.T) {
	testCases := []struct{ sds string }{
		{sds: "istio"},
		{sds: "spire"},
	}
	for _, tc := range testCases {
		t.Run(tc.sds, func(t *testing.T) {
			configDumpFile, err := os.Open(fmt.Sprintf("testdata/secret/%s/config_dump.json", tc.sds))
			if err != nil {
				t.Errorf("error opening test data file: %v", err)
			}
			defer configDumpFile.Close()
			configDump, err := io.ReadAll(configDumpFile)
			if err != nil {
				t.Errorf("error reading test data file: %v", err)
			}

			outFile, err := os.Open(fmt.Sprintf("testdata/secret/%s/output", tc.sds))
			if err != nil {
				t.Errorf("error opening test data output file: %v", err)
			}
			defer outFile.Close()
			expectedOut, err := io.ReadAll(outFile)
			if err != nil {
				t.Errorf("error reading test data output file: %v", err)
			}

			gotOut := &bytes.Buffer{}
			cw := &ConfigWriter{Stdout: gotOut}
			err = cw.Prime(configDump)
			assert.NoError(t, err)
			err = cw.PrintSecretSummary()
			assert.NoError(t, err)

			assert.Equal(t, string(expectedOut), gotOut.String())
		})
	}
}
