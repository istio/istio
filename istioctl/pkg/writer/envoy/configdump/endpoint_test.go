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
	"os"
	"path"
	"testing"

	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/test/util/assert"
)

func TestPrintEndpointsSummary(t *testing.T) {
	tests := []struct {
		name   string
		filter EndpointFilter
	}{
		{
			name:   "emptyfilter",
			filter: EndpointFilter{},
		},
		{
			name: "portfilter",
			filter: EndpointFilter{
				Port: 8080,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOut := &bytes.Buffer{}
			cw := &ConfigWriter{Stdout: gotOut}
			cd, _ := os.ReadFile("testdata/endpoint/configdump.json")
			cw.Prime(cd)
			err := cw.PrintEndpointsSummary(tt.filter)
			assert.NoError(t, err)

			wantOutputFile := path.Join("testdata/endpoint", fmt.Sprintf("%s_output.txt", tt.name))
			util.CompareContent(t, gotOut.Bytes(), wantOutputFile)
		})
	}
}

func TestPrintEndpoints(t *testing.T) {
	tests := []struct {
		name         string
		outputFormat string
		filter       EndpointFilter
	}{
		{
			name:         "emptyfilter",
			outputFormat: "json",
			filter:       EndpointFilter{},
		},
		{
			name:         "emptyfilter",
			outputFormat: "yaml",
			filter:       EndpointFilter{},
		},
		{
			name:         "portfilter",
			outputFormat: "json",
			filter: EndpointFilter{
				Port: 8080,
			},
		},
		{
			name:         "portfilter",
			outputFormat: "yaml",
			filter: EndpointFilter{
				Port: 8080,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOut := &bytes.Buffer{}
			cw := &ConfigWriter{Stdout: gotOut}
			cd, _ := os.ReadFile("testdata/endpoint/configdump.json")
			cw.Prime(cd)
			err := cw.PrintEndpoints(tt.filter, tt.outputFormat)
			assert.NoError(t, err)

			wantOutputFile := path.Join("testdata/endpoint", fmt.Sprintf("%s_output.%s", tt.name, tt.outputFormat))
			util.CompareContent(t, gotOut.Bytes(), wantOutputFile)
		})
	}
}
