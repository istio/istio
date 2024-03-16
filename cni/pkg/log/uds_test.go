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
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/util/assert"
)

func TestUDSLog(t *testing.T) {
	// Start UDS log server
	udsSockDir := t.TempDir()
	udsSock := filepath.Join(udsSockDir, "cni.sock")
	logger := NewUDSLogger()
	pluginLog.SetOutputLevel(log.DebugLevel) // this will be configured by global.logging.level
	stop := make(chan struct{})
	defer close(stop)
	assert.NoError(t, logger.StartUDSLogServer(udsSock, stop))

	// Configure log to tee to UDS server
	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	loggingOptions := log.DefaultOptions()
	loggingOptions.WithTeeToUDS(udsSock, constants.UDSLogPath)
	assert.NoError(t, log.Configure(loggingOptions))
	log.FindScope("default").SetOutputLevel(log.DebugLevel)
	log.Debug("debug log")
	log.Info("info log")
	log.Warn("warn log")
	log.Error("error log")
	// This will error because stdout cannot sync, but the UDS part should sync
	// Ideally we would fail if the UDS part fails but the error library makes it kind of tricky
	_ = log.Sync()

	// Restore os stdout.
	os.Stdout = stdout
	assert.NoError(t, log.Configure(loggingOptions))

	assert.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	assert.NoError(t, err)

	// For each level, there should be two lines, one from direct log,
	// the other one from UDS server
	wantLevels := []string{"debug", "info", "warn", "error", "debug", "info", "warn", "error"}
	gotLogs := strings.Split(
		strings.TrimSuffix(string(out), "\n"), "\n")
	if want, got := len(wantLevels), len(gotLogs); want != got {
		t.Fatalf("Number of logs want %v, got %v logs: %v", want, got, gotLogs)
	}

	for i, l := range gotLogs {
		// For each line, there should be two level string, e.g.
		// "2021-07-09T03:26:08.984951Z	debug	debug log"
		if got, want := strings.Count(l, wantLevels[i]), 2; want != got {
			t.Errorf("Number of log level string want %v, got %v", want, got)
		}
	}
}
