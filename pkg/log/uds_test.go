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
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type udsServer struct {
	messages []string
}

func (us *udsServer) handleLog(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := io.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	messages := []string{}
	if err := json.Unmarshal(body, &messages); err != nil {
		w.WriteHeader(http.StatusBadRequest)
	}
	us.messages = append(us.messages, messages...)
}

func TestUDSLog(t *testing.T) {
	srv := udsServer{messages: make([]string, 0)}
	udsDir := t.TempDir()
	socketPath := filepath.Join(udsDir, "test.sock")
	unixListener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create uds listener: %v", err)
	}
	loggingOptions := DefaultOptions()
	loggingOptions.JSONEncoding = true
	if err := Configure(loggingOptions.WithTeeToUDS(socketPath, "/")); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleLog)

	go func() {
		if err := http.Serve(unixListener, mux); err != nil {
			t.Error(err)
		}
	}()

	{
		t.Log("test sending normal log")

		WithLabels("k", "v").Info("test")
		Warn("test2")
		Sync()

		// There should be two messages received at server
		// {"msg":"test","k":"v"}
		// {"msg":"test2"}
		if got, want := len(srv.messages), 2; got != want {
			t.Fatalf("number received log messages got %v want %v", got, want)
		}
		type testMessage struct {
			Msg string `json:"msg"`
			K   string `json:"k"`
		}

		want := []testMessage{
			{Msg: "test", K: "v"},
			{Msg: "test2"},
		}
		got := make([]testMessage, 2)
		json.Unmarshal([]byte(srv.messages[0]), &got[0])
		json.Unmarshal([]byte(srv.messages[1]), &got[1])
		if !reflect.DeepEqual(got, want) {
			t.Errorf("received log messages, got %v want %v", got, want)
		}
	}

	{
		t.Log("test sending log with specified time")

		// Clean up all the mssages, and log again. Check that buffer is cleaned up properly.
		srv.messages = make([]string, 0)
		yesterday := time.Now().Add(-time.Hour * 24).Truncate(time.Microsecond)
		defaultScope.LogWithTime(InfoLevel, "test3", yesterday)
		Sync()
		// There should only be one message in the buffer
		if got, want := len(srv.messages), 1; got != want {
			t.Fatalf("number received log messages got %v want %v", got, want)
		}

		type testMessage struct {
			Msg  string    `json:"msg"`
			Time time.Time `json:"time"`
		}
		var got testMessage
		json.Unmarshal([]byte(srv.messages[0]), &got)
		if got.Time.UnixNano() != yesterday.UnixNano() {
			t.Errorf("received log message with specified time, got %v want %v", got.Time, yesterday)
		}
	}
}
