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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

// An udsCore write entries to an UDS server with HTTP Post. Log messages will be encoded into a JSON array.
type udsCore struct {
	client       http.Client
	minimumLevel zapcore.Level
	url          string
	enc          zapcore.Encoder
	buffers      []*buffer.Buffer
	mu           sync.Mutex
}

// teeToUDSServer returns a zapcore.Core that writes entries to both the provided core and to an uds server.
func teeToUDSServer(baseCore zapcore.Core, address, path string) zapcore.Core {
	c := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", address)
			},
		},
		Timeout: 100 * time.Millisecond,
	}
	uc := &udsCore{
		client:  c,
		url:     "http://unix" + path,
		enc:     zapcore.NewJSONEncoder(defaultEncoderConfig),
		buffers: make([]*buffer.Buffer, 0),
	}
	for l := zapcore.DebugLevel; l <= zapcore.FatalLevel; l++ {
		if baseCore.Enabled(l) {
			uc.minimumLevel = l
			break
		}
	}
	return zapcore.NewTee(baseCore, uc)
}

// Enabled implements zapcore.Core.
func (u *udsCore) Enabled(l zapcore.Level) bool {
	return l >= u.minimumLevel
}

// With implements zapcore.Core.
func (u *udsCore) With(fields []zapcore.Field) zapcore.Core {
	return &udsCore{
		client:       u.client,
		minimumLevel: u.minimumLevel,
	}
}

// Check implements zapcore.Core.
func (u *udsCore) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if u.Enabled(e.Level) {
		return ce.AddCore(e, u)
	}
	return ce
}

// Sync implements zapcore.Core. It sends log messages with HTTP POST.
func (u *udsCore) Sync() error {
	logs := u.logsFromBuffer()
	msg, err := json.Marshal(logs)
	if err != nil {
		return fmt.Errorf("failed to sync uds log: %v", err)
	}
	resp, err := u.client.Post(u.url, "application/json", bytes.NewReader(msg))
	if err != nil {
		return fmt.Errorf("failed to send logs to uds server %v: %v", u.url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("uds server returns non-ok status %v: %v", u.url, resp.Status)
	}
	return nil
}

// Write implements zapcore.Core. Log messages will be temporarily buffered and sent to
// UDS server asyncrhonously.
func (u *udsCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	buffer, err := u.enc.EncodeEntry(entry, fields)
	if err != nil {
		return fmt.Errorf("failed to write log to uds logger: %v", err)
	}
	u.mu.Lock()
	u.buffers = append(u.buffers, buffer)
	u.mu.Unlock()
	return nil
}

func (u *udsCore) logsFromBuffer() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	logs := make([]string, 0, len(u.buffers))
	for _, b := range u.buffers {
		logs = append(logs, b.String())
		b.Free()
	}
	u.buffers = make([]*buffer.Buffer, 0)
	return logs
}
