// Copyright 2018 The Operator-SDK Authors
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

package eventapi

import (
	"fmt"
	"strings"
	"time"
)

const (
	// Ansible Events

	// EventPlaybookOnTaskStart - playbook is starting to run a task.
	EventPlaybookOnTaskStart = "playbook_on_task_start"
	// EventRunnerOnOk - task finished with ok status.
	EventRunnerOnOk = "runner_on_ok"
	// EventRunnerOnFailed - task finished with failed status.
	EventRunnerOnFailed = "runner_on_failed"
	// EventPlaybookOnStats - playbook has finished running.
	EventPlaybookOnStats = "playbook_on_stats"

	// Ansible Task Actions

	// TaskActionSetFact - task action of setting a fact.
	TaskActionSetFact = "set_fact"
	// TaskActionDebug - task action of printing a debug message.
	TaskActionDebug = "debug"

	// defaultFailedMessage - Default failed playbook message
	defaultFailedMessage = "unknown playbook failure"
)

// EventTime - time to unmarshal nano time.
type EventTime struct {
	time.Time
}

// UnmarshalJSON - override unmarshal json.
func (e *EventTime) UnmarshalJSON(b []byte) (err error) {
	e.Time, err = time.Parse("2006-01-02T15:04:05.999999999", strings.Trim(string(b[:]), "\"\\"))
	return
}

// MarshalJSON - override the marshal json.
func (e EventTime) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", e.Time.Format("2006-01-02T15:04:05.99999999"))), nil
}

// JobEvent - event of an ansible run.
type JobEvent struct {
	UUID      string                 `json:"uuid"`
	Counter   int                    `json:"counter"`
	StdOut    string                 `json:"stdout"`
	StartLine int                    `json:"start_line"`
	EndLine   int                    `json:"EndLine"`
	Event     string                 `json:"event"`
	EventData map[string]interface{} `json:"event_data"`
	PID       int                    `json:"pid"`
	Created   EventTime              `json:"created"`
}

// StatusJobEvent - event of an ansible run.
type StatusJobEvent struct {
	UUID      string         `json:"uuid"`
	Counter   int            `json:"counter"`
	StdOut    string         `json:"stdout"`
	StartLine int            `json:"start_line"`
	EndLine   int            `json:"EndLine"`
	Event     string         `json:"event"`
	EventData StatsEventData `json:"event_data"`
	PID       int            `json:"pid"`
	Created   EventTime      `json:"created"`
}

// StatsEventData - data for a the status event.
type StatsEventData struct {
	Playbook     string         `json:"playbook"`
	PlaybookUUID string         `json:"playbook_uuid"`
	Changed      map[string]int `json:"changed"`
	Ok           map[string]int `json:"ok"`
	Failures     map[string]int `json:"failures"`
	Skipped      map[string]int `json:"skipped"`
}

// FailureMessages - failure messages from the event api
type FailureMessages []string

// GetFailedPlaybookMessage - get the failure message from res.msg
func (je JobEvent) GetFailedPlaybookMessage() string {
	message := defaultFailedMessage
	result, ok := je.EventData["res"].(map[string]interface{})
	if !ok {
		return message
	}
	if m, ok := result["msg"].(string); ok {
		message = m
	}
	return message
}

// IgnoreError - Does the job event contain the ignore_error ansible flag
func (je JobEvent) IgnoreError() bool {
	ignoreErrors, ok := je.EventData["ignore_errors"]
	if !ok {
		return false
	}
	if b, ok := ignoreErrors.(bool); ok && b {
		return b
	}
	return false
}
