// Copyright 2018 Istio Authors.
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

package pilot

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
)

var statusTimeFormat = "2006-01-02 15:04:05.999999999 UTC"

// StatusWriter enables printing of sync status using multiple []byte Pilot responses
type StatusWriter struct {
	Writer io.Writer
}

// PrintAll takes a slice of Pilot syncz responses and outputs them using a tabwriter
func (s *StatusWriter) PrintAll(statuses map[string][]byte) error {
	w, fullStatus, err := s.setupStatusPrint(statuses)
	if err != nil {
		return err
	}
	for _, status := range fullStatus {
		if err := statusPrintln(w, status); err != nil {
			return err
		}
	}
	return w.Flush()
}

// PrintSingle takes a slice of Pilot syncz responses and outputs them using a tabwriter filtering for a specific pod
func (s *StatusWriter) PrintSingle(statuses map[string][]byte, podName string) error {
	w, fullStatus, err := s.setupStatusPrint(statuses)
	if err != nil {
		return err
	}
	for _, status := range fullStatus {
		if strings.Contains(status.ProxyID, podName) {
			if err := statusPrintln(w, status); err != nil {
				return err
			}
		}
	}
	return w.Flush()
}

func (s *StatusWriter) setupStatusPrint(statuses map[string][]byte) (*tabwriter.Writer, []v2.SyncStatus, error) {
	w := new(tabwriter.Writer)
	w.Init(s.Writer, 0, 8, 5, '\t', 0)
	fmt.Fprintln(w, "PROXY\tSTATUS\tSENT\tACKNOWLEDGED")
	fullStatus := []v2.SyncStatus{}
	for _, status := range statuses {
		ss := []v2.SyncStatus{}
		err := json.Unmarshal(status, &ss)
		if err != nil {
			return nil, nil, err
		}
		fullStatus = append(fullStatus, ss...)
	}
	sort.Slice(fullStatus, func(i, j int) bool {
		return fullStatus[i].ProxyID < fullStatus[j].ProxyID
	})
	return w, fullStatus, nil
}

func parseMonotonicTime(t string) (time.Time, error) {
	split := strings.Split(t, " m=") // strip monotonic clock suffix
	return time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", split[0])
}

func statusPrintln(w io.Writer, status v2.SyncStatus) error {
	sent, err := parseMonotonicTime(status.Sent)
	if err != nil {
		return err
	}
	acked, err := parseMonotonicTime(status.Acked)
	if err != nil {
		return err
	}
	synced := "DIVERGED"
	if sent == acked {
		synced = "SYNCED"
	}
	fmt.Fprintf(w, "%v\t%v\t%v\t%v\n", status.ProxyID, synced, sent.Format(statusTimeFormat), acked.Format(statusTimeFormat))
	return nil
}
