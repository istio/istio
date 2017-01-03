// Copyright 2016 Google Inc.
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

package jsonLogger

import (
	"bytes"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"istio.io/mixer/pkg/adapter"
)

var (
	t, _ = time.Parse("2006-Jan-01", "2016-Dec-06")

	textEntry = adapter.LogEntry{
		Collection:  "info_log",
		Timestamp:   t,
		Severity:    "INFO",
		TextPayload: "Server start",
	}

	structEntry = adapter.LogEntry{
		Collection: "access_log",
		Timestamp:  t,
		Severity:   "ERROR",
		StructPayload: map[string]interface{}{
			"source_ip":      "10.0.0.1",
			"source_user":    "tester@test.com",
			"api_method":     "GetShelves",
			"response_code":  "404",
			"response_bytes": 43545,
		},
	}
)

func TestLog(t *testing.T) {
	tests := []struct {
		name      string
		entries   []adapter.LogEntry
		want      []string
		expectErr bool
	}{
		{
			name:    "Single Report",
			entries: []adapter.LogEntry{textEntry},
			want:    []string{"{\"logsCollection\":\"info_log\",\"timestamp\":\"2016-06-01T00:00:00Z\",\"severity\":\"INFO\",\"textPayload\":\"Server start\"}"},
		},
		{
			name:    "Batch Reports",
			entries: []adapter.LogEntry{textEntry, structEntry},
			want: []string{
				"{\"logsCollection\":\"info_log\",\"timestamp\":\"2016-06-01T00:00:00Z\",\"severity\":\"INFO\",\"textPayload\":\"Server start\"}",
				`{"logsCollection":"access_log",` +
					`"timestamp":"2016-06-01T00:00:00Z",` +
					`"severity":"ERROR",` +
					`"structPayload":{"api_method":"GetShelves","response_bytes":43545,"response_code":"404","source_ip":"10.0.0.1","source_user":"tester@test.com"}}`,
			},
		},
	}

	b := NewAdapter()
	if err := b.Configure(b.DefaultAdapterConfig()); err != nil {
		t.Errorf("Unable to configure adapter: %v", err)
	}

	for _, v := range tests {
		a, err := b.NewAspect(b.DefaultAspectConfig())
		if err != nil {
			if !v.expectErr {
				t.Errorf("%s - could not build adapter: %v", v.name, err)
			}
			continue
		}
		if err == nil && v.expectErr {
			t.Errorf("%s - expected error building adapter", v.name)
		}

		l := a.(adapter.Logger)

		old := os.Stdout // for restore
		r, w, _ := os.Pipe()
		os.Stdout = w // redirecting
		// copy over the output from stderr
		outC := make(chan string)
		go func() {
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r); err != nil {
				t.Errorf("io.Copy failed: %v", err)
			}
			outC <- buf.String()
		}()

		if err := l.Log(v.entries); err != nil {
			t.Errorf("Logging failed: %v", err)
		}

		// back to normal state
		if err := w.Close(); err != nil {
			t.Errorf("w.Close failed: %v", err)
		}
		os.Stdout = old
		logs := <-outC

		got := strings.Split(strings.TrimSpace(logs), "\n")

		if !reflect.DeepEqual(got, v.want) {
			t.Errorf("%s - got %s, want %s", v.name, got, v.want)
		}
	}
}
