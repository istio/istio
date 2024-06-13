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

package log

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"istio.io/istio/cni/pkg/constants"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test/util/assert"
)

func TestUDSLog(t *testing.T) {
	// Start UDS log server
	udsSockDir := t.TempDir()
	udsSock := filepath.Join(udsSockDir, "cni.sock")
	logger := NewUDSLogger(istiolog.DebugLevel)
	stop := make(chan struct{})
	defer close(stop)
	assert.NoError(t, logger.StartUDSLogServer(udsSock, stop))

	// Configure log to tee to UDS server
	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	loggingOptions := istiolog.DefaultOptions()
	loggingOptions.JSONEncoding = true
	loggingOptions.WithTeeToUDS(udsSock, constants.UDSLogPath)
	assert.NoError(t, istiolog.Configure(loggingOptions))
	istiolog.FindScope("default").SetOutputLevel(istiolog.DebugLevel)
	istiolog.Debug("debug log")
	istiolog.Info("info log")
	istiolog.Warn("warn log")
	istiolog.Error("error log")
	istiolog.WithLabels("key", 2).Infof("with labels")
	// This will error because stdout cannot sync, but the UDS part should sync
	// Ideally we would fail if the UDS part fails but the error library makes it kind of tricky
	_ = istiolog.Sync()

	// Restore os stdout.
	os.Stdout = stdout
	assert.NoError(t, istiolog.Configure(loggingOptions))

	assert.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	assert.NoError(t, err)

	cases := []struct {
		level string
		msg   string
		key   *float64
	}{
		{"debug", "debug log", nil},
		{"info", "info log", nil},
		{"warn", "warn log", nil},
		{"error", "error log", nil},
		{"info", "with labels", ptr.Of(float64(2))},
	}
	// For each level, there should be two lines, one from direct log,
	// the other one from UDS server
	gotLogs := strings.Split(
		strings.TrimSuffix(string(out), "\n"), "\n")
	if want, got := len(cases)*2, len(gotLogs); want != got {
		t.Fatalf("Number of logs want %v, got %v logs: %v", want, got, gotLogs)
	}
	i := 0
	for _, l := range gotLogs {
		var parsedLog map[string]any
		assert.NoError(t, json.Unmarshal([]byte(l), &parsedLog))
		if parsedLog["scope"] != "cni-plugin" {
			// Each log is 2x: one direct, and one over UDS. Just test the UDS one
			continue
		}
		// remove scope since it is constant and not needed to test
		delete(parsedLog, "scope")
		// check time is there
		if _, f := parsedLog["time"]; !f {
			t.Fatalf("log %v did not have time", i)
		}
		// but remove time since it changes on each test
		delete(parsedLog, "time")
		want := map[string]any{
			"level": cases[i].level,
			"msg":   cases[i].msg,
		}
		if k := cases[i].key; k != nil {
			want["key"] = *k
		}
		assert.Equal(t, want, parsedLog)
		i++
	}
}

func TestParseCniLog(t *testing.T) {
	wantT := &time.Time{}
	assert.NoError(t, wantT.UnmarshalText([]byte("2020-01-01T00:00:00.356374Z")))
	cases := []struct {
		name string
		in   string
		out  cniLog
	}{
		{
			"without keys",
			`{"level":"info","time":"2020-01-01T00:00:00.356374Z","msg":"my message"}`,
			cniLog{
				Level:     "info",
				Time:      *wantT,
				Msg:       "my message",
				Arbitrary: nil,
			},
		},
		{
			"with keys",
			`{"level":"info","time":"2020-01-01T00:00:00.356374Z","msg":"my message","key":"string value","bar":2}`,
			cniLog{
				Level: "info",
				Time:  *wantT,
				Msg:   "my message",
				Arbitrary: map[string]any{
					"key": "string value",
					"bar": float64(2),
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := parseCniLog(tt.in)
			assert.Equal(t, got, tt.out)
		})
	}
}
