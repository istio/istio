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
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/pkg/uds"
	"istio.io/pkg/log"
)

var pluginLog = log.RegisterScope("cni", "CNI network plugin", 0)

type UDSLogger struct {
	mu            sync.Mutex
	loggingServer *http.Server
}

type cniLog struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

func NewUDSLogger() *UDSLogger {
	l := &UDSLogger{}
	mux := http.NewServeMux()
	mux.HandleFunc(constants.UDSLogPath, l.handleLog)
	loggingServer := &http.Server{
		Handler: mux,
	}
	l.loggingServer = loggingServer
	return l
}

// StartUDSServer starts up a UDS server which receives log reported from CNI network plugin.
func (l *UDSLogger) StartUDSLogServer(sockAddress string, stop <-chan struct{}) error {
	if sockAddress == "" {
		return nil
	}
	log.Info("Start a UDS server for CNI plugin logs")
	unixListener, err := uds.NewListener(sockAddress)
	if err != nil {
		return fmt.Errorf("failed to create UDS listener: %v", err)
	}
	go func() {
		if err := l.loggingServer.Serve(unixListener); err != nil &&
			err != http.ErrServerClosed {
			log.Errorf("Error running UDS log server: %v", err)
		}
	}()

	go func() {
		<-stop
		if err := l.loggingServer.Close(); err != nil {
			log.Errorf("CNI log server terminated with error: %v", err)
		} else {
			log.Debug("CNI log server terminated")
		}
	}()

	return nil
}

func (l *UDSLogger) handleLog(w http.ResponseWriter, req *http.Request) {
	if req.Body == nil {
		return
	}
	defer req.Body.Close()
	data, err := io.ReadAll(req.Body)
	if err != nil {
		log.Errorf("Failed to read log report from cni plugin: %v", err)
		return
	}
	l.processLog(data)
}

func (l *UDSLogger) processLog(body []byte) {
	cniLogs := make([]string, 0)
	err := json.Unmarshal(body, &cniLogs)
	if err != nil {
		log.Errorf("Failed to unmarshal CNI plugin logs: %v", err)
		return
	}
	messages := make([]cniLog, 0, len(cniLogs))
	for _, l := range cniLogs {
		var msg cniLog
		if err := json.Unmarshal([]byte(l), &msg); err != nil {
			log.Debugf("Failed to unmarshal CNI plugin log entry: %v", err)
			continue
		}
		msg.Msg = strings.TrimSpace(msg.Msg)
		messages = append(messages, msg)
	}
	// Lock log message printing to prevent log messages from different CNI
	// processes interleave.
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, m := range messages {
		// There is no fatal log from CNI plugin
		switch m.Level {
		case "debug":
			pluginLog.Debug(m.Msg)
		case "info":
			pluginLog.Info(m.Msg)
		case "warn":
			pluginLog.Warn(m.Msg)
		case "error":
			pluginLog.Error(m.Msg)
		}
	}
}
