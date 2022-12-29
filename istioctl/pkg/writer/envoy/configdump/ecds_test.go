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
	"encoding/json"
	"os"
	"testing"

	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/test/util/assert"
)

func TestPrintEcdsSummary(t *testing.T) {
	gotOut := &bytes.Buffer{}
	cw := &ConfigWriter{Stdout: gotOut}
	cd, _ := os.ReadFile("testdata/ecds/configdump.json")
	cw.Prime(cd)
	err := cw.PrintEcdsSummary()
	assert.NoError(t, err)

	util.CompareContent(t, gotOut.Bytes(), "testdata/ecds/output.txt")
}

func TestPrintEcdsYaml(t *testing.T) {
	gotOut := &bytes.Buffer{}
	cw := &ConfigWriter{Stdout: gotOut}
	cd, _ := os.ReadFile("testdata/ecds/configdump.json")
	cw.Prime(cd)
	err := cw.PrintEcds("yaml")
	assert.NoError(t, err)

	util.CompareContent(t, gotOut.Bytes(), "testdata/ecds/output.yaml")
}

func TestPrintEcdsJSON(t *testing.T) {
	gotOut := &bytes.Buffer{}
	cw := &ConfigWriter{Stdout: gotOut}
	cd, _ := os.ReadFile("testdata/ecds/configdump.json")
	cw.Prime(cd)
	err := cw.PrintEcds("json")
	assert.NoError(t, err)

	// protojson opt out of whitespace randomization, see more details: https://github.com/golang/protobuf/issues/1082
	var rm json.RawMessage = gotOut.Bytes()
	jsonOutput, err := json.MarshalIndent(rm, "", "    ")
	if err != nil {
		assert.NoError(t, err)
	}
	jsonOutput = append(jsonOutput, '\n')

	util.CompareContent(t, jsonOutput, "testdata/ecds/output.json")
}
