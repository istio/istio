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
	"time"

	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/scopes"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/uds"
)

var (
	pluginLog = scopes.CNIPlugin
	log       = scopes.CNIAgent
)

type UDSLogger struct {
	mu            sync.Mutex
	loggingServer *http.Server
}

type cniLog struct {
	Level     string         `json:"level"`
	Time      time.Time      `json:"time"`
	Msg       string         `json:"msg"`
	Arbitrary map[string]any `json:"-"`
}

func NewUDSLogger(level istiolog.Level) *UDSLogger {
	l := &UDSLogger{}
	mux := http.NewServeMux()
	mux.HandleFunc(constants.UDSLogPath, l.handleLog)
	loggingServer := &http.Server{
		Handler: mux,
	}
	l.loggingServer = loggingServer
	pluginLog.SetOutputLevel(level)
	return l
}

// StartUDSLogServer starts up a UDS server which receives log reported from CNI network plugin.
func (l *UDSLogger) StartUDSLogServer(sockAddress string, stop <-chan struct{}) error {
	if sockAddress == "" {
		return nil
	}
	log.Debugf("starting UDS server for CNI plugin logs")
	unixListener, err := uds.NewListener(sockAddress)
	if err != nil {
		return fmt.Errorf("failed to create UDS listener: %v", err)
	}
	go func() {
		if err := l.loggingServer.Serve(unixListener); network.IsUnexpectedListenerError(err) {
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
		msg, ok := parseCniLog(l)
		if !ok {
			continue
		}
		messages = append(messages, msg)
	}
	// Lock log message printing to prevent log messages from different CNI
	// processes interleave.
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, m := range messages {
		logger := pluginLog
		// For any k/v pairs, add them back
		for k, v := range m.Arbitrary {
			logger = logger.WithLabels(k, v)
		}
		// There is no fatal log from CNI plugin
		switch m.Level {
		case "debug":
			logger.LogWithTime(istiolog.DebugLevel, m.Msg, m.Time)
		case "info":
			logger.LogWithTime(istiolog.InfoLevel, m.Msg, m.Time)
		case "warn":
			logger.LogWithTime(istiolog.WarnLevel, m.Msg, m.Time)
		case "error":
			logger.LogWithTime(istiolog.ErrorLevel, m.Msg, m.Time)
		}
	}
}

// parseCniLog is tricky because we have known and arbitrary fields in the log, and Go doesn't make that very easy
func parseCniLog(l string) (cniLog, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(l), &raw); err != nil {
		log.Debugf("Failed to unmarshal CNI plugin log entry: %v", err)
		return cniLog{}, false
	}
	var msg cniLog
	if err := json.Unmarshal(raw["msg"], &msg.Msg); err != nil {
		log.Debugf("Failed to unmarshal CNI plugin log entry: %v", err)
		return cniLog{}, false
	}
	if err := json.Unmarshal(raw["level"], &msg.Level); err != nil {
		log.Debugf("Failed to unmarshal CNI plugin log entry: %v", err)
		return cniLog{}, false
	}
	if err := json.Unmarshal(raw["time"], &msg.Time); err != nil {
		log.Debugf("Failed to unmarshal CNI plugin log entry: %v", err)
		return cniLog{}, false
	}
	delete(raw, "msg")
	delete(raw, "level")
	delete(raw, "time")
	msg.Arbitrary = make(map[string]any, len(raw))
	for k, v := range raw {
		var res any
		if err := json.Unmarshal(v, &res); err != nil {
			log.Debugf("Failed to unmarshal CNI plugin log entry: %v", err)
			continue
		}
		msg.Arbitrary[k] = res
	}
	msg.Msg = strings.TrimSpace(msg.Msg)
	return msg, true
}
