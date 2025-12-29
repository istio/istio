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

package processlog

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/tools/bug-report/pkg/config"
)

func TestProcessLogsFormat(t *testing.T) {
	testDataDir := filepath.Join(env.IstioSrc, "tools/bug-report/pkg/testdata/")

	tests := []struct {
		name              string
		inputLogFilePath  string
		wantOutputLogPath string
		startTime         string
		endTime           string
		timeFilterApplied bool
	}{
		{
			name:              "input_log_of_text_format",
			inputLogFilePath:  "input/format_txt.log",
			wantOutputLogPath: "output/format_txt_no_time_filter.log",
			timeFilterApplied: false,
		},
		{
			name:              "input_log_of_json_format",
			inputLogFilePath:  "input/format_json.log",
			wantOutputLogPath: "output/format_json_no_time_filter.log",
			timeFilterApplied: false,
		},
		{
			name:              "input_log_of_text_format",
			inputLogFilePath:  "input/format_txt.log",
			wantOutputLogPath: "output/format_txt_with_time_filter.log",
			startTime:         "2020-06-29T23:37:27.336155Z",
			endTime:           "2020-06-29T23:37:27.349559Z",
			timeFilterApplied: true,
		},
		{
			name:              "input_log_of_json_format",
			inputLogFilePath:  "input/format_json.log",
			wantOutputLogPath: "output/format_json_with_time_filter.log",
			startTime:         "2023-05-10T17:43:55.356647Z",
			endTime:           "2023-05-10T17:43:55.356691Z",
			timeFilterApplied: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputLog := string(util.ReadFile(t, filepath.Join(testDataDir, tt.inputLogFilePath)))
			wantOutputLog := string(util.ReadFile(t, filepath.Join(testDataDir, tt.wantOutputLogPath)))
			start, _ := time.Parse(time.RFC3339Nano, tt.startTime)
			end, _ := time.Parse(time.RFC3339Nano, tt.endTime)
			c := config.BugReportConfig{StartTime: start, EndTime: end, TimeFilterApplied: tt.timeFilterApplied}
			gotOutputLog, _ := Process(&c, inputLog)
			if wantOutputLog != gotOutputLog {
				t.Errorf("got:\n%s\nwant:\n%s\n\ndiff (-got, +want):\n%s\n", gotOutputLog, wantOutputLog, cmp.Diff(gotOutputLog, wantOutputLog))
			}
		})
	}
}
