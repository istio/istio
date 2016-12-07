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

package adapters

import (
	"encoding/json"
	"io"
	"time"
)

type (
	// Logger is the interface for adapters that will handle logs data within
	// the mixer.
	Logger interface {
		Adapter

		// Log directs a backend adapter to process a batch of LogEntries derived
		// from potentially several Report() calls.
		Log([]LogEntry) error
	}

	// LogEntry is the top-level struct for logs data that will be
	// exported by logging adapters.
	LogEntry struct {
		// Collection identifies the logs target for the LogEntry.
		Collection string `json:"logsCollection,omitempty"`
		// Timestamp identifies the event time for the log.
		Timestamp time.Time `json:"timestamp,omitempty"`
		// Labels are a set of key-value pairs for non-payload logs data.
		Labels map[string]string `json:"labels,omitempty"`
		// Severity indicates the log-level for the entry.
		Severity string `json:"severity,omitempty"`
		// ProtoPayload is for providing a protobuf-encoded payload.
		ProtoPayload string `json:"protoPayload,omitempty"`
		// TextPayload is for providing a raw text payload.
		TextPayload string `json:"textPayload,omitempty"`
		// StructPayload is for providing a structured text payload (JSON).
		StructPayload map[string]interface{} `json:"structPayload,omitempty"`
	}
)

// WriteJSON converts a log entry to json and writes to the supplied io.Writer
func WriteJSON(w io.Writer, l LogEntry) error {
	if err := json.NewEncoder(w).Encode(l); err != nil {
		return err
	}
	return nil
}
